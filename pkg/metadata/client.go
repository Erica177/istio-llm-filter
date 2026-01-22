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

// Package metadata 提供 Metadata-Center 客户端实现
package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/envoyproxy/envoy/contrib/golang/common/go/api"

	"github.com/istio-llm-filter/pkg/types"
)

// API 路径常量
const (
	// LoadStatsPath 负载统计 API 路径
	LoadStatsPath = "/v1/load/stats"
	// LoadPromptPath 负载 Prompt 长度 API 路径
	LoadPromptPath = "/v1/load/prompt"
	// CacheQueryPath 缓存查询 API 路径
	CacheQueryPath = "/v1/cache/query"
	// CacheSavePath 缓存保存 API 路径
	CacheSavePath = "/v1/cache/save"

	// TraceIdHeader Trace ID 请求头
	TraceIdHeader = "TraceId"

	// DefaultTopK 默认返回 Top K 缓存位置
	DefaultTopK = 10
	// DefaultChunkLen 默认分块长度
	DefaultChunkLen = 512
)

// 环境变量名称
const (
	EnvMetadataCenterHost    = "METADATA_CENTER_HOST"
	EnvMetadataCenterPort    = "METADATA_CENTER_PORT"
	EnvFetchMetricTimeout    = "METADATA_CENTER_FETCH_METRIC_TIMEOUT"
	EnvFetchCacheTimeout     = "METADATA_CENTER_FETCH_CACHE_TIMEOUT"
	EnvUpdateStatsTimeout    = "METADATA_CENTER_UPDATE_STATS_TIMEOUT"
	EnvClientTimeout         = "METADATA_CENTER_CLIENT_TIMEOUT"
	EnvClientMaxIdleConns    = "METADATA_CENTER_CLIENT_MAX_IDLE_CONNS"
	EnvClientKeepAlive       = "METADATA_CENTER_CLIENT_KEEPALIVE"
	EnvQueueSize             = "METADATA_CENTER_QUEUE_SIZE"
	EnvWorkerCount           = "METADATA_CENTER_WORKER_COUNT"
	EnvMaxFailoverRetry      = "METADATA_CENTER_MAX_FAILOVER_RETRY"
)

// Context 键
const (
	// CtxKeyTraceId 用于在 Context 中传递 Trace ID
	CtxKeyTraceId types.LBCtxKey = "metacenter.traceId"
)

var (
	// 全局单例
	globalClient     *Client
	globalClientOnce sync.Once
	globalClientMu   sync.RWMutex

	// 配置缓存
	fetchMetricTimeout     = 100 // ms
	fetchMetricTimeoutOnce sync.Once
	fetchCacheTimeout      = 100 // ms
	fetchCacheTimeoutOnce  sync.Once

	// 是否启用 metadata center
	metadataCenterEnabled     bool
	metadataCenterEnabledOnce sync.Once
	metadataCenterCacheEnabled     bool
	metadataCenterCacheEnabledOnce sync.Once
)

// 请求/响应类型定义

// ErrorInfo 错误信息
type ErrorInfo struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
}

// Response 通用响应结构
type Response struct {
	Status  string     `json:"status"`
	Error   *ErrorInfo `json:"error"`
	TraceID string     `json:"trace_id,omitempty"`
}

// InferenceRequest 推理请求统计
type InferenceRequest struct {
	RequestId    string `json:"request_id"`
	Cluster      string `json:"cluster"`
	PromptLength int    `json:"prompt_length,omitempty"`
	Ip           string `json:"ip"`
	TimeStamp    int64  `json:"timestamp,omitempty"`
	TraceId      string `json:"-"`
}

// EngineStats 引擎负载统计
type EngineStats struct {
	Ip           string `json:"ip"`
	QueuedReqNum int32  `json:"queued_req_num"`
	PromptLength int32  `json:"prompt_length"`
	UpdatedTime  int64  `json:"updated_time"`
}

// CacheQueryParam 缓存查询参数
type CacheQueryParam struct {
	Cluster    string   `json:"cluster"`
	PromptHash []uint64 `json:"prompt_hash"`
	TopK       int      `json:"topk"`
}

// LocationResponse 缓存位置响应
type LocationResponse struct {
	Ip     string `json:"ip"`
	Length int    `json:"length"`
}

// CacheQueryResponse 缓存查询响应
type CacheQueryResponse struct {
	Locations []*LocationResponse `json:"locations"`
}

// CacheSaveParam 缓存保存参数
type CacheSaveParam struct {
	Cluster    string   `json:"cluster"`
	PromptHash []uint64 `json:"prompt_hash"`
	Ip         string   `json:"ip"`
}

// RequestParam 请求参数
type RequestParam struct {
	TraceId string
	HashKey string
	Method  string
	Path    string
	Query   map[string]string
	Body    []byte
	Timeout time.Duration
}

// Client Metadata-Center 客户端
type Client struct {
	httpClient    *http.Client
	asyncQueue    *AsyncQueue
	host          string
	port          int
	failoverRetry int
}

// GetClient 获取全局 Metadata-Center 客户端
func GetClient() types.MetadataCenter {
	globalClientMu.RLock()
	if globalClient != nil {
		globalClientMu.RUnlock()
		return globalClient
	}
	globalClientMu.RUnlock()

	globalClientMu.Lock()
	defer globalClientMu.Unlock()
	if globalClient != nil {
		return globalClient
	}

	globalClient = NewClient()
	return globalClient
}

// IsEnabled 检查 Metadata-Center 是否启用
func IsEnabled() bool {
	metadataCenterEnabledOnce.Do(func() {
		host := os.Getenv(EnvMetadataCenterHost)
		metadataCenterEnabled = host != ""
	})
	return metadataCenterEnabled
}

// IsCacheEnabled 检查 KV-Cache 是否启用
func IsCacheEnabled() bool {
	metadataCenterCacheEnabledOnce.Do(func() {
		// 默认与 metadata center 一起启用
		metadataCenterCacheEnabled = IsEnabled()
	})
	return metadataCenterCacheEnabled
}

// NewClient 创建新的 Metadata-Center 客户端
func NewClient() *Client {
	host := getEnvString(EnvMetadataCenterHost, "localhost")
	port := getEnvInt(EnvMetadataCenterPort, 80)
	timeout := getEnvDuration(EnvClientTimeout, 100*time.Millisecond)
	keepAlive := getEnvDuration(EnvClientKeepAlive, 10*time.Second)
	maxIdleConns := getEnvInt(EnvClientMaxIdleConns, 1024)
	failoverRetry := getEnvInt(EnvMaxFailoverRetry, 1)

	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: keepAlive,
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			MaxIdleConns:        maxIdleConns,
			MaxConnsPerHost:     maxIdleConns,
			MaxIdleConnsPerHost: maxIdleConns,
			IdleConnTimeout:     5 * time.Minute,
		},
	}

	// 创建异步队列
	queueSize := getEnvInt(EnvQueueSize, 1000)
	workerCount := getEnvInt(EnvWorkerCount, 100)
	updateTimeout := getEnvDuration(EnvUpdateStatsTimeout, 100*time.Millisecond)

	client := &Client{
		httpClient:    httpClient,
		host:          host,
		port:          port,
		failoverRetry: failoverRetry,
	}

	client.asyncQueue = NewAsyncQueue(queueSize, workerCount, updateTimeout, client)

	api.LogInfof("metadata center client initialized, host=%s, port=%d", host, port)
	return client
}

// AddRequest 添加请求统计（异步）
func (c *Client) AddRequest(ctx context.Context, requestId, cluster, ip string, promptLength int) error {
	traceId := types.GetValueFromCtx(ctx, CtxKeyTraceId, "")
	req := &InferenceRequest{
		RequestId:    requestId,
		Cluster:      cluster,
		Ip:           ip,
		PromptLength: promptLength,
		TimeStamp:    time.Now().UnixNano(),
		TraceId:      traceId,
	}

	body, err := json.Marshal(req)
	if err != nil {
		api.LogErrorf("json marshal error, req:%v, err:%v", req, err)
		return err
	}

	task := &Task{
		HashKey: req.Cluster,
		Method:  http.MethodPost,
		URL:     LoadStatsPath,
		Body:    body,
		TraceId: req.TraceId,
	}

	if err := c.asyncQueue.Dispatch(task); err != nil {
		api.LogErrorf("add request stats failed, req:%v, err:%v", req, err)
		return err
	}
	api.LogDebugf("add request stats, req:%v", req)
	return nil
}

// DeleteRequest 删除请求统计（异步）
func (c *Client) DeleteRequest(ctx context.Context, requestId string) error {
	traceId := types.GetValueFromCtx(ctx, CtxKeyTraceId, "")
	req := &InferenceRequest{
		RequestId: requestId,
		TraceId:   traceId,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	task := &Task{
		Method:  http.MethodDelete,
		URL:     LoadStatsPath,
		Body:    body,
		TraceId: req.TraceId,
	}

	if err := c.asyncQueue.Dispatch(task); err != nil {
		api.LogErrorf("delete request stats failed, req:%v, err:%v", req, err)
		return err
	}
	api.LogDebugf("delete request stats, req:%v", req)
	return nil
}

// DeleteRequestPrompt 删除请求 Prompt 长度（异步）
func (c *Client) DeleteRequestPrompt(ctx context.Context, requestId string) error {
	traceId := types.GetValueFromCtx(ctx, CtxKeyTraceId, "")
	req := &InferenceRequest{
		RequestId: requestId,
		TraceId:   traceId,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	task := &Task{
		Method:  http.MethodDelete,
		URL:     LoadPromptPath,
		Body:    body,
		TraceId: req.TraceId,
	}

	if err := c.asyncQueue.Dispatch(task); err != nil {
		api.LogErrorf("delete prompt length failed, req:%v, err:%v", req, err)
		return err
	}
	api.LogDebugf("delete prompt length, req:%v", req)
	return nil
}

// QueryLoad 查询集群负载统计（同步）
func (c *Client) QueryLoad(ctx context.Context, cluster string) (map[string]*types.EndpointStats, error) {
	traceId := types.GetValueFromCtx(ctx, CtxKeyTraceId, "")
	fetchMetricTimeoutOnce.Do(func() {
		fetchMetricTimeout = getEnvInt(EnvFetchMetricTimeout, 100)
	})

	body, err := c.doRequest(ctx, RequestParam{
		TraceId: traceId,
		HashKey: cluster,
		Method:  http.MethodGet,
		Path:    LoadStatsPath,
		Query:   map[string]string{"cluster": cluster},
		Timeout: time.Duration(fetchMetricTimeout) * time.Millisecond,
	})

	if err != nil {
		api.LogErrorf("query load failed, err: %v", err)
		return nil, fmt.Errorf("failed to query load: %v", err)
	}

	type metricResponse struct {
		Response
		ModelStats []EngineStats `json:"data"`
	}
	var response metricResponse
	if err := json.Unmarshal(body, &response); err != nil {
		api.LogErrorf("parse metric response error: %v", err)
		return nil, err
	}

	stats := make(map[string]*types.EndpointStats, len(response.ModelStats))
	for _, engine := range response.ModelStats {
		promptLen := int(engine.PromptLength)
		if promptLen < 0 {
			api.LogErrorf("query load: %s prompt length is negative, len: %d", engine.Ip, promptLen)
			promptLen = 0
		}
		stats[engine.Ip] = &types.EndpointStats{
			PromptLength: promptLen,
			PrefillReqs:  0,
			TotalReqs:    int(engine.QueuedReqNum),
		}
	}
	api.LogDebugf("metadata center load response:%s", string(body))

	return stats, nil
}

// QueryKVCache 查询 KV 缓存位置（同步）
func (c *Client) QueryKVCache(ctx context.Context, cluster string, promptHash []uint64, topK int) ([]*types.KVCacheLocation, error) {
	fetchCacheTimeoutOnce.Do(func() {
		fetchCacheTimeout = getEnvInt(EnvFetchCacheTimeout, 100)
	})

	param := &CacheQueryParam{
		Cluster:    cluster,
		PromptHash: promptHash,
		TopK:       topK,
	}

	reqBody, err := json.Marshal(param)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal cache request: %v", err)
	}

	body, err := c.doRequest(ctx, RequestParam{
		TraceId: types.GetValueFromCtx(ctx, CtxKeyTraceId, ""),
		HashKey: param.Cluster,
		Method:  http.MethodPost,
		Path:    CacheQueryPath,
		Body:    reqBody,
		Timeout: time.Duration(fetchCacheTimeout) * time.Millisecond,
	})

	if err != nil {
		api.LogErrorf("query cache failed, err:%v", err)
		return nil, fmt.Errorf("failed to query cache: %v", err)
	}

	type cacheQueryResp struct {
		Data CacheQueryResponse `json:"data"`
		Response
	}
	var response cacheQueryResp
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("parse cache response error: %s", err.Error())
	}
	if len(response.Data.Locations) == 0 {
		return nil, nil
	}

	stats := make([]*types.KVCacheLocation, 0, len(response.Data.Locations))
	for _, item := range response.Data.Locations {
		stats = append(stats, &types.KVCacheLocation{
			Ip:     item.Ip,
			Length: item.Length,
		})
	}

	api.LogDebugf("metadata center cache stats:%v", stats)
	return stats, nil
}

// SaveKVCache 保存 KV 缓存位置（异步）
func (c *Client) SaveKVCache(ctx context.Context, cluster, ip string, promptHash []uint64) error {
	traceId := types.GetValueFromCtx(ctx, CtxKeyTraceId, "")
	req := &CacheSaveParam{
		Cluster:    cluster,
		Ip:         ip,
		PromptHash: promptHash,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	task := &Task{
		HashKey: req.Cluster,
		Method:  http.MethodPost,
		URL:     CacheSavePath,
		Body:    body,
		TraceId: traceId,
	}

	if err := c.asyncQueue.Dispatch(task); err != nil {
		api.LogErrorf("save cache failed, req:%+v, err:%+v", req, err)
		return err
	}

	api.LogDebugf("save cache, trace_id:%s, cluster:%s, ip:%v", traceId, cluster, ip)
	return nil
}

// doRequest 执行 HTTP 请求
func (c *Client) doRequest(ctx context.Context, reqParam RequestParam) ([]byte, error) {
	newCtx, cancel := context.WithTimeout(ctx, reqParam.Timeout)
	defer cancel()

	var bodyReader io.Reader
	if reqParam.Body != nil {
		bodyReader = bytes.NewReader(reqParam.Body)
	}

	reqUrl := fmt.Sprintf("http://%s:%d%s", c.host, c.port, reqParam.Path)
	req, err := http.NewRequestWithContext(newCtx, reqParam.Method, reqUrl, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	if reqParam.Body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = int64(len(reqParam.Body))
	}
	req.Header.Set(TraceIdHeader, reqParam.TraceId)

	if reqParam.Query != nil {
		q := req.URL.Query()
		for k, v := range reqParam.Query {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %v, body: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// HandleRequest 处理异步请求（供 AsyncQueue 调用）
func (c *Client) HandleRequest(ctx context.Context, task *Task) error {
	_, err := c.doRequest(ctx, RequestParam{
		TraceId: task.TraceId,
		HashKey: task.HashKey,
		Method:  task.Method,
		Path:    task.URL,
		Body:    task.Body,
		Timeout: task.Timeout,
	})
	return err
}

// 辅助函数

func getEnvString(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		// 尝试解析为毫秒
		if ms, err := strconv.Atoi(v); err == nil {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return defaultValue
}

// 创建一个空的 MetadataCenter 实现，用于禁用时使用
type noopClient struct{}

func (n *noopClient) AddRequest(ctx context.Context, requestId, cluster, ip string, promptLength int) error {
	return nil
}

func (n *noopClient) DeleteRequest(ctx context.Context, requestId string) error {
	return nil
}

func (n *noopClient) DeleteRequestPrompt(ctx context.Context, requestId string) error {
	return nil
}

func (n *noopClient) QueryLoad(ctx context.Context, cluster string) (map[string]*types.EndpointStats, error) {
	return nil, errors.New("metadata center disabled")
}

func (n *noopClient) SaveKVCache(ctx context.Context, cluster, ip string, promptHash []uint64) error {
	return nil
}

func (n *noopClient) QueryKVCache(ctx context.Context, cluster string, promptHash []uint64, topK int) ([]*types.KVCacheLocation, error) {
	return nil, errors.New("metadata center disabled")
}

// GetClientOrNoop 获取客户端，如果 metadata center 未启用则返回空实现
func GetClientOrNoop() types.MetadataCenter {
	if !IsEnabled() {
		return &noopClient{}
	}
	return GetClient()
}
