// Copyright The AIGW Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package filter 实现 LLM Proxy Golang Filter
package filter

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"mosn.io/htnn/api/pkg/filtermanager/api"

	"github.com/istio-llm-filter/pkg/config"
	"github.com/istio-llm-filter/pkg/hash"
	"github.com/istio-llm-filter/pkg/loadbalancer"
	"github.com/istio-llm-filter/pkg/metadata"
	"github.com/istio-llm-filter/pkg/transcoder"
	_ "github.com/istio-llm-filter/pkg/transcoder/openai" // 注册 OpenAI 转码器
	"github.com/istio-llm-filter/pkg/types"
)

// Name 过滤器名称
const Name = "llm-proxy"

var hostname = os.Getenv("HOSTNAME")

// Filter LLM Proxy 过滤器
type Filter struct {
	api.PassThroughFilter
	callbacks api.FilterCallbackHandler
	config    *config.LLMProxyConfig

	// 转码器
	transcoder transcoder.Transcoder

	// 请求上下文
	traceId         string
	modelName       string
	cluster         string
	serverIp        string
	backendProtocol string
	isStream        bool
	uniqueId        string

	// Metadata-Center 相关
	promptLength          int
	promptHash            []uint64
	isIncreaseRecorded    bool
	isPromptLengthDeleted bool
	promptDecreaseTimer   *time.Timer

	// 响应处理
	respHeader   api.ResponseHeaderMap
	dropRespData bool

	// 时间统计
	sendFinishTimestamp int64
	firstTokenTimestamp int64
	lastTokenTimestamp  int64
}

// UniqueId 获取请求唯一 ID
func (f *Filter) UniqueId() string {
	if f.uniqueId == "" {
		f.uniqueId = uuid.New().String()
	}
	return f.uniqueId
}

// DecodeHeaders 处理请求头
// 返回 WaitAllData 等待完整请求体
func (f *Filter) DecodeHeaders(header api.RequestHeaderMap, endStream bool) api.ResultAction {
	if endStream {
		// 没有请求体，返回错误
		return f.badRequest(fmt.Errorf("no request body"))
	}
	return api.WaitAllData
}

// DecodeRequest 处理完整请求
// 这是请求处理的核心逻辑
func (f *Filter) DecodeRequest(headers api.RequestHeaderMap, buffer api.BufferInstance, trailers api.RequestTrailerMap) api.ResultAction {
	// 1. 获取 Trace ID
	f.traceId = getTraceID(f.callbacks, headers)

	// 2. 获取转码器
	inputProtocol := f.config.GetProtocol()
	factory := transcoder.GetFactory(inputProtocol)
	if factory == nil {
		return f.badRequest(fmt.Errorf("transcoder not found for protocol %s", inputProtocol))
	}
	f.transcoder = factory(f.callbacks, f.config)

	// 3. 解析请求数据
	reqData, err := f.transcoder.GetRequestData(headers, buffer.Bytes())
	if err != nil {
		return f.badRequest(err)
	}

	// 4. 提取请求信息
	f.modelName = reqData.ModelName
	f.backendProtocol = reqData.BackendProtocol
	f.cluster = reqData.Cluster

	api.LogDebugf("[TraceID: %s] request: model=%s, cluster=%s, backend=%s",
		f.traceId, f.modelName, f.cluster, f.backendProtocol)

	// 5. 计算 Prompt 哈希
	f.computePromptHash(reqData.PromptContext)

	// 6. 初始化负载均衡上下文
	ctx := f.initLoadBalanceContext()

	// 7. 选择后端服务器
	algorithm := types.LoadBalancerType(f.config.GetAlgorithm())
	hosts := f.getClusterHosts()
	if len(hosts) == 0 {
		return f.noUpstream(fmt.Errorf("no hosts in cluster %s", f.cluster))
	}

	host, err := loadbalancer.ChooseServer(ctx, f.cluster, algorithm, hosts)
	if err != nil {
		api.LogErrorf("[TraceID: %s] choose server failed: %v", f.traceId, err)
		return f.noUpstream(err)
	}

	f.serverIp = host.Ip()
	api.LogInfof("[TraceID: %s] selected backend: %s for cluster %s",
		f.traceId, f.serverIp, f.cluster)

	// 8. 转码请求
	proxyModelName := f.modelName
	if reqData.LbOptions != nil && reqData.LbOptions.GetLoraID() != "" {
		proxyModelName = reqData.LbOptions.GetLoraID()
	}

	reqCtx, err := f.transcoder.EncodeRequest(proxyModelName, f.backendProtocol, headers, buffer)
	if err != nil {
		return f.badRequest(err)
	}

	f.isStream = reqCtx.IsStream

	// 9. 更新 Metadata-Center（异步）
	f.addRequest()

	// 10. 记录发送完成时间
	f.sendFinishTimestamp = time.Now().UnixMicro()

	// 11. 设置上游主机（通过 Dynamic Metadata 或直接设置）
	f.setUpstreamHost(headers, host)

	return api.Continue
}

// EncodeHeaders 处理响应头
func (f *Filter) EncodeHeaders(header api.ResponseHeaderMap, endStream bool) api.ResultAction {
	// 添加通用响应头
	if hostname != "" {
		header.Add("x-llm-proxy-via", hostname)
	}

	status, _ := header.Status()
	if status >= http.StatusBadRequest {
		// 错误响应，等待完整响应体
		api.LogInfof("[TraceID: %s] error response status=%d", f.traceId, status)
		return api.WaitAllData
	}

	// 保存 KV-Cache（仅 200 响应）
	f.saveKVCache()

	// 解码响应头
	if err := f.transcoder.DecodeHeaders(header); err != nil {
		return f.badResponse(err)
	}

	if endStream {
		return f.badResponse(fmt.Errorf("no response data"))
	}

	if f.isStream {
		// 流式响应
		header.Set("content-type", "text/event-stream;charset=UTF-8")
		header.Set("x-accel-buffering", "no")
		f.respHeader = header
		return api.WaitData
	}

	// 非流式响应
	header.Set("content-type", "application/json")
	return api.WaitAllData
}

// EncodeResponse 处理非流式完整响应
func (f *Filter) EncodeResponse(headers api.ResponseHeaderMap, buffer api.BufferInstance, trailers api.ResponseTrailerMap) api.ResultAction {
	// 删除 Prompt 长度
	f.deletePromptLength()

	status, _ := headers.Status()
	if status >= http.StatusBadRequest {
		// 错误响应，保留原样
		api.LogWarnf("[TraceID: %s] error response status=%d, body=%s",
			f.traceId, status, buffer.String())
		return api.Continue
	}

	return f.processResponseData(headers, buffer)
}

// EncodeData 处理流式响应数据
func (f *Filter) EncodeData(buffer api.BufferInstance, endStream bool) api.ResultAction {
	// 记录首 Token 时间
	f.recordTokenTime()

	return f.processResponseData(f.respHeader, buffer)
}

// OnLog 请求完成回调
func (f *Filter) OnLog(reqHeaders api.RequestHeaderMap, reqTrailers api.RequestTrailerMap,
	respHeaders api.ResponseHeaderMap, respTrailers api.ResponseTrailerMap) {

	// 删除请求统计
	if f.isIncreaseRecorded {
		f.decreaseRequest()
	}

	// 记录日志指标
	ttft := f.getTTFT()
	api.LogInfof("[TraceID: %s] request completed: model=%s, backend=%s, ttft=%dms",
		f.traceId, f.modelName, f.serverIp, ttft.Milliseconds())
}

// 内部方法

func (f *Filter) badRequest(err error) api.ResultAction {
	api.LogInfof("[TraceID: %s] bad request: %v", f.traceId, err)
	hdr := http.Header{}
	hdr.Set("Content-Type", "application/json")
	return &api.LocalResponse{
		Code:   http.StatusBadRequest,
		Msg:    string(types.FormatGatewayResponse(&types.ErrBadRequest, f.traceId, err.Error())),
		Header: hdr,
	}
}

func (f *Filter) noUpstream(err error) api.ResultAction {
	api.LogInfof("[TraceID: %s] no upstream: %v", f.traceId, err)
	hdr := http.Header{}
	hdr.Set("Content-Type", "application/json")
	return &api.LocalResponse{
		Code:   http.StatusNotFound,
		Msg:    string(types.FormatGatewayResponse(&types.ErrNotFound, f.traceId, err.Error())),
		Header: hdr,
	}
}

func (f *Filter) badResponse(err error) api.ResultAction {
	api.LogInfof("[TraceID: %s] bad response: %v", f.traceId, err)
	hdr := http.Header{}
	hdr.Set("Content-Type", "application/json")
	return &api.LocalResponse{
		Code:   http.StatusServiceUnavailable,
		Msg:    string(types.FormatGatewayResponse(&types.ErrInferenceServer, f.traceId, err.Error())),
		Header: hdr,
	}
}

func (f *Filter) initLoadBalanceContext() context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.KeyTraceId, f.traceId)
	ctx = context.WithValue(ctx, types.KeyModelName, f.modelName)
	ctx = context.WithValue(ctx, types.KeyClusterName, f.cluster)
	ctx = context.WithValue(ctx, metadata.CtxKeyTraceId, f.traceId)

	// 设置 Prompt 哈希
	if len(f.promptHash) > 0 {
		ctx = context.WithValue(ctx, types.KeyPromptHash, f.promptHash)
	}

	// 设置负载均衡配置
	if lbConfig := f.config.FindLbMappingRule(f.modelName); lbConfig != nil {
		ctx = context.WithValue(ctx, types.KeyLoadAwareEnable, lbConfig.LoadAwareEnable)
		ctx = context.WithValue(ctx, types.KeyCacheAwareEnable, lbConfig.CacheAwareEnable)
		ctx = context.WithValue(ctx, types.KeyCandidatePercent, int(lbConfig.CandidatePercent))
		ctx = context.WithValue(ctx, types.KeyLoadRequestWeight, int(lbConfig.RequestLoadWeight))
		ctx = context.WithValue(ctx, types.KeyLoadPrefillWeight, int(lbConfig.PrefillLoadWeight))
		ctx = context.WithValue(ctx, types.KeyCacheRatioWeight, int(lbConfig.CacheRadioWeight))
	}

	return ctx
}

func (f *Filter) computePromptHash(promptCtx *types.PromptMessageContext) {
	if promptCtx == nil {
		return
	}
	f.promptLength = len(promptCtx.PromptContent)

	// 检查是否启用缓存感知
	if !f.isCacheAwareEnabled() {
		return
	}

	if len(promptCtx.PromptContent) > 0 {
		h := hash.New(&hash.Config{ChunkLen: hash.DefaultTextChunkLen})
		f.promptHash = h.PromptToHash(promptCtx.PromptContent)
	}

	api.LogDebugf("[TraceID: %s] prompt hash computed: length=%d, hash_count=%d",
		f.traceId, f.promptLength, len(f.promptHash))
}

func (f *Filter) getClusterHosts() []types.Host {
	// TODO: 从 Envoy 集群管理器获取主机列表
	// 这里需要通过 Envoy API 获取实际的集群端点
	// 暂时返回空，实际实现需要集成 Envoy 集群发现
	return nil
}

func (f *Filter) setUpstreamHost(headers api.RequestHeaderMap, host types.Host) {
	// 设置上游主机
	// 可以通过设置特定的请求头或 Dynamic Metadata 来指定上游
	if host != nil {
		headers.Set("x-upstream-host", host.Address())
	}
}

func (f *Filter) processResponseData(headers api.ResponseHeaderMap, buffer api.BufferInstance) api.ResultAction {
	if f.dropRespData {
		buffer.Reset()
		return api.Continue
	}

	inputBuf := buffer.Bytes()
	outputBuf, err := f.transcoder.GetResponseData(inputBuf)
	if err != nil {
		api.LogWarnf("[TraceID: %s] response transcoding error: %v", f.traceId, err)
		if f.isStream {
			// 流式响应出错，设置错误状态并丢弃后续数据
			headers.Set(":status", "400")
			f.isStream = false
			f.dropRespData = true
		}
	}

	if !f.isStream && headers != nil {
		headers.Set("content-length", fmt.Sprintf("%d", len(outputBuf)))
	}

	if outputBuf == nil {
		buffer.Reset()
	} else {
		_ = buffer.Set(outputBuf)
	}

	return api.Continue
}

func (f *Filter) recordTokenTime() {
	now := time.Now().UnixMicro()
	if f.firstTokenTimestamp == 0 {
		f.firstTokenTimestamp = now
		// 首 Token 到达，删除 Prompt 长度
		f.deletePromptLength()
	}
	f.lastTokenTimestamp = now
}

func (f *Filter) getTTFT() time.Duration {
	if f.firstTokenTimestamp <= 0 || f.sendFinishTimestamp <= 0 {
		return 0
	}
	if f.firstTokenTimestamp < f.sendFinishTimestamp {
		return 0
	}
	return time.Duration(f.firstTokenTimestamp-f.sendFinishTimestamp) * time.Microsecond
}

func (f *Filter) isLoadAwareEnabled() bool {
	if lbConfig := f.config.FindLbMappingRule(f.modelName); lbConfig != nil {
		return lbConfig.LoadAwareEnable
	}
	return metadata.IsEnabled()
}

func (f *Filter) isCacheAwareEnabled() bool {
	if lbConfig := f.config.FindLbMappingRule(f.modelName); lbConfig != nil {
		return lbConfig.CacheAwareEnable
	}
	return metadata.IsCacheEnabled()
}

// Metadata-Center 操作

func (f *Filter) addRequest() {
	if !f.isLoadAwareEnabled() {
		return
	}

	ctx := context.WithValue(context.Background(), metadata.CtxKeyTraceId, f.traceId)
	client := metadata.GetClientOrNoop()
	err := client.AddRequest(ctx, f.UniqueId(), f.cluster, f.serverIp, f.promptLength)
	if err != nil {
		api.LogErrorf("[TraceID: %s] add request failed: %v", f.traceId, err)
		return
	}

	f.isIncreaseRecorded = true
	api.LogDebugf("[TraceID: %s] add request: cluster=%s, ip=%s, prompt_length=%d",
		f.traceId, f.cluster, f.serverIp, f.promptLength)

	// 非流式请求设置定时器删除 Prompt 长度
	if !f.isStream {
		// 预估 TTFT 的 1.2 倍后删除
		ttftMs := estimateTTFT(f.modelName, f.promptLength)
		delay := time.Duration(ttftMs*12/10) * time.Millisecond
		f.promptDecreaseTimer = time.AfterFunc(delay, func() {
			f.deletePromptLength()
		})
	}
}

func (f *Filter) deletePromptLength() {
	if !f.isLoadAwareEnabled() || !f.isIncreaseRecorded || f.isPromptLengthDeleted {
		return
	}

	if f.promptDecreaseTimer != nil {
		f.promptDecreaseTimer.Stop()
		f.promptDecreaseTimer = nil
	}

	ctx := context.WithValue(context.Background(), metadata.CtxKeyTraceId, f.traceId)
	client := metadata.GetClientOrNoop()
	err := client.DeleteRequestPrompt(ctx, f.UniqueId())
	if err != nil {
		api.LogErrorf("[TraceID: %s] delete prompt length failed: %v", f.traceId, err)
		return
	}

	f.isPromptLengthDeleted = true
	api.LogDebugf("[TraceID: %s] delete prompt length", f.traceId)
}

func (f *Filter) decreaseRequest() {
	if !f.isLoadAwareEnabled() {
		return
	}

	if f.promptDecreaseTimer != nil {
		f.promptDecreaseTimer.Stop()
		f.promptDecreaseTimer = nil
	}

	ctx := context.WithValue(context.Background(), metadata.CtxKeyTraceId, f.traceId)
	client := metadata.GetClientOrNoop()
	err := client.DeleteRequest(ctx, f.UniqueId())
	if err != nil {
		api.LogErrorf("[TraceID: %s] delete request failed: %v", f.traceId, err)
	}
	api.LogDebugf("[TraceID: %s] delete request", f.traceId)
}

func (f *Filter) saveKVCache() {
	if !f.isCacheAwareEnabled() || len(f.promptHash) == 0 {
		return
	}

	ctx := context.WithValue(context.Background(), metadata.CtxKeyTraceId, f.traceId)
	client := metadata.GetClientOrNoop()
	err := client.SaveKVCache(ctx, f.cluster, f.serverIp, f.promptHash)
	if err != nil {
		api.LogErrorf("[TraceID: %s] save kv cache failed: %v", f.traceId, err)
	}
	api.LogDebugf("[TraceID: %s] save kv cache: cluster=%s, ip=%s",
		f.traceId, f.cluster, f.serverIp)
}

// 辅助函数

func getTraceID(callbacks api.FilterCallbackHandler, headers api.RequestHeaderMap) string {
	// 尝试从请求头获取
	if traceId, ok := headers.Get("x-request-id"); ok && traceId != "" {
		return traceId
	}
	if traceId, ok := headers.Get("x-trace-id"); ok && traceId != "" {
		return traceId
	}
	// 生成新的 Trace ID
	return uuid.New().String()
}

func estimateTTFT(modelName string, promptLength int) int {
	// 简单的 TTFT 估算
	// 实际实现可以基于历史数据进行更精确的估算
	baseTTFT := 100 // ms
	lengthFactor := promptLength / 1000
	return baseTTFT + lengthFactor*50
}
