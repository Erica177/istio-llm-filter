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

// Package openai 提供 OpenAI 协议的转码器实现
package openai

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/bytedance/sonic"
	"github.com/envoyproxy/envoy/contrib/golang/common/go/api"

	"github.com/istio-llm-filter/pkg/config"
	"github.com/istio-llm-filter/pkg/transcoder"
	"github.com/istio-llm-filter/pkg/types"
)

// 后端协议类型
const (
	BackendVLLM     = "vllm"
	BackendSGLang   = "sglang"
	BackendTensorRT = "tensorrt"
	BackendTriton   = "triton"
)

// SSE 数据前缀和结束标记
var (
	sseDataPrefix = []byte("data: ")
	sseDoneMarker = []byte("[DONE]")
	sseNewline    = []byte("\n\n")
)

func init() {
	// 注册 OpenAI 转码器工厂
	transcoder.RegisterFactory("openai", NewTranscoder)
}

// ChatCompletionRequest OpenAI 聊天完成请求
type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	TopP        *float64      `json:"top_p,omitempty"`
	N           *int          `json:"n,omitempty"`
	Stop        interface{}   `json:"stop,omitempty"`
}

// ChatMessage 聊天消息
type ChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // 可以是 string 或 []ContentPart
}

// ContentPart 多模态内容部分
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL 图片 URL
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// Transcoder OpenAI 协议转码器
type Transcoder struct {
	callbacks api.FilterCallbackHandler
	config    *config.LLMProxyConfig

	// 解析后的请求
	request ChatCompletionRequest

	// 请求上下文
	modelName       string
	backendProtocol string
	isStream        bool

	// 日志项
	logItems types.LLMLogItems
}

// NewTranscoder 创建 OpenAI 转码器
func NewTranscoder(callbacks api.FilterCallbackHandler, cfg *config.LLMProxyConfig) transcoder.Transcoder {
	return &Transcoder{
		callbacks: callbacks,
		config:    cfg,
	}
}

// GetRequestData 解析请求数据
func (t *Transcoder) GetRequestData(headers api.RequestHeaderMap, data []byte) (*types.RequestData, error) {
	// 解析 JSON 请求
	if err := sonic.Unmarshal(data, &t.request); err != nil {
		return nil, fmt.Errorf("failed to parse request: %w", err)
	}

	// 验证请求
	if len(t.request.Messages) == 0 {
		return nil, errors.New("messages is empty")
	}
	if t.request.Model == "" {
		return nil, errors.New("model is empty")
	}

	reqData := &types.RequestData{
		ModelName: t.request.Model,
	}

	// 查找模型映射规则
	if t.config != nil && len(t.config.ModelMappings) > 0 {
		tuples := config.GetModelMappings(t.config.ModelMappings, t.request.Model)
		if len(tuples) == 0 {
			return nil, fmt.Errorf("model %s not found in mapping rules", t.request.Model)
		}

		// 根据请求头选择规则
		targetRule := config.GetCandidateRule(tuples, headers)
		if targetRule == nil {
			return nil, fmt.Errorf("no matching rule found for model %s", t.request.Model)
		}

		t.modelName = targetRule.SceneName
		reqData.SceneName = targetRule.SceneName
		reqData.BackendProtocol = targetRule.Backend
		reqData.Cluster = targetRule.Cluster

		// 构建负载均衡选项
		reqData.LbOptions = buildLbOptions(targetRule)
	}

	// 提取 Prompt 内容
	reqData.PromptContext = t.extractPromptContext()

	t.logItems.ModelName = reqData.ModelName
	api.LogDebugf("OpenAI request parsed: model=%s, cluster=%s, backend=%s",
		reqData.ModelName, reqData.Cluster, reqData.BackendProtocol)

	return reqData, nil
}

// EncodeRequest 编码请求到后端格式
func (t *Transcoder) EncodeRequest(modelName, backendProtocol string, headers api.RequestHeaderMap, buffer api.BufferInstance) (*types.RequestContext, error) {
	t.isStream = t.request.Stream
	t.backendProtocol = backendProtocol

	reqCtx := &types.RequestContext{
		IsStream: t.isStream,
	}

	// 对于 vLLM、SGLang、TensorRT 后端，直接转发原始请求
	switch backendProtocol {
	case BackendVLLM, BackendSGLang, BackendTensorRT:
		return reqCtx, nil
	}

	// 其他后端可能需要转码（如 gRPC）
	// 目前仅支持直接转发
	return reqCtx, nil
}

// DecodeHeaders 解码响应头
func (t *Transcoder) DecodeHeaders(headers api.ResponseHeaderMap) error {
	// OpenAI 响应头通常不需要特殊处理
	return nil
}

// GetResponseData 转码响应数据
func (t *Transcoder) GetResponseData(data []byte) ([]byte, error) {
	// 对于直接转发的后端，响应也直接转发
	switch t.backendProtocol {
	case BackendVLLM, BackendSGLang, BackendTensorRT:
		return t.processOpenAIResponse(data)
	}

	return data, nil
}

// GetLLMLogItems 获取日志项
func (t *Transcoder) GetLLMLogItems() *types.LLMLogItems {
	return &t.logItems
}

// extractPromptContext 提取 Prompt 上下文
func (t *Transcoder) extractPromptContext() *types.PromptMessageContext {
	ctx := &types.PromptMessageContext{
		IsVlModel: false,
	}

	// 收集所有消息内容
	var contentBuilder bytes.Buffer
	for _, msg := range t.request.Messages {
		switch content := msg.Content.(type) {
		case string:
			contentBuilder.WriteString(content)
		case []interface{}:
			// 多模态内容
			for _, part := range content {
				if partMap, ok := part.(map[string]interface{}); ok {
					if partMap["type"] == "text" {
						if text, ok := partMap["text"].(string); ok {
							contentBuilder.WriteString(text)
						}
					} else if partMap["type"] == "image_url" {
						// 标记为 VL 模型
						ctx.IsVlModel = true
					}
				}
			}
		}
	}

	ctx.PromptContent = contentBuilder.Bytes()
	return ctx
}

// processOpenAIResponse 处理 OpenAI 格式响应
func (t *Transcoder) processOpenAIResponse(data []byte) ([]byte, error) {
	if !t.isStream {
		// 非流式响应，解析并验证
		return t.processNonStreamResponse(data)
	}

	// 流式响应，处理 SSE 格式
	return t.processStreamResponse(data)
}

// processNonStreamResponse 处理非流式响应
func (t *Transcoder) processNonStreamResponse(data []byte) ([]byte, error) {
	// 检查是否是错误响应
	if bytes.Contains(data, []byte(`"error"`)) {
		// 保留错误响应原样返回
		return data, nil
	}

	// 解析响应以提取 token 统计信息
	var resp struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := sonic.Unmarshal(data, &resp); err == nil {
		t.logItems.InputTokens = resp.Usage.PromptTokens
		t.logItems.OutputTokens = resp.Usage.CompletionTokens
	}

	return data, nil
}

// processStreamResponse 处理流式响应
func (t *Transcoder) processStreamResponse(data []byte) ([]byte, error) {
	// SSE 格式数据，直接转发
	// 格式: data: {...}\n\n 或 data: [DONE]\n\n

	// 检查是否是错误响应（第一个 chunk 可能是错误）
	if bytes.HasPrefix(data, []byte("{")) && bytes.Contains(data, []byte(`"error"`)) {
		return nil, fmt.Errorf("stream error: %s", string(data))
	}

	return data, nil
}

// buildLbOptions 构建负载均衡选项
func buildLbOptions(rule *config.Rule) *types.LoadBalancerOptions {
	if rule == nil {
		return nil
	}

	opts := &types.LoadBalancerOptions{
		RouteName: rule.RouteName,
	}

	// 提取 headers
	if len(rule.Headers) > 0 {
		opts.Headers = make(map[string]string)
		for _, h := range rule.Headers {
			opts.Headers[h.Key] = h.Value
		}
	}

	// 提取 subset 标签和 LoRA
	if len(rule.Subset) > 0 {
		opts.Selector = make(map[string]string)
		for _, s := range rule.Subset {
			if s.Lora != "" {
				opts.LoraID = s.Lora
			}
			for k, v := range s.Labels {
				opts.Selector[k] = v
			}
		}
	}

	return opts
}
