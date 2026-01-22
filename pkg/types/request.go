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

package types

// RequestData 表示解析后的请求数据
type RequestData struct {
	// ModelName 客户端请求的模型名称
	ModelName string
	// SceneName 场景名称（路由规则映射后的名称）
	SceneName string
	// Env 环境标识
	Env string
	// Cluster 目标集群名称
	Cluster string
	// BackendProtocol 后端协议类型 (vllm, sglang, triton 等)
	BackendProtocol string
	// LbOptions 负载均衡选项
	LbOptions *LoadBalancerOptions
	// PromptContext Prompt 上下文信息
	PromptContext *PromptMessageContext
}

// LoadBalancerOptions 负载均衡选项
type LoadBalancerOptions struct {
	// RouteName 路由名称
	RouteName string
	// LoraID LoRA 适配器 ID
	LoraID string
	// Headers 请求头匹配条件
	Headers map[string]string
	// Selector 标签选择器
	Selector map[string]string
}

// GetLoraID 获取 LoRA ID
func (o *LoadBalancerOptions) GetLoraID() string {
	if o == nil {
		return ""
	}
	return o.LoraID
}

// GetHeaderString 获取请求头字符串表示
func (o *LoadBalancerOptions) GetHeaderString() string {
	if o == nil || len(o.Headers) == 0 {
		return ""
	}
	result := ""
	for k, v := range o.Headers {
		result += k + "=" + v + ","
	}
	return result
}

// GetSubsetString 获取子集选择器字符串表示
func (o *LoadBalancerOptions) GetSubsetString() string {
	if o == nil || len(o.Selector) == 0 {
		return ""
	}
	result := ""
	for k, v := range o.Selector {
		result += k + "=" + v + ","
	}
	return result
}

// PromptMessageContext Prompt 消息上下文
type PromptMessageContext struct {
	// IsVlModel 是否是视觉语言模型
	IsVlModel bool
	// PromptContent Prompt 内容（用于哈希计算）
	PromptContent []byte
}

// RequestContext 请求上下文
type RequestContext struct {
	// IsStream 是否是流式请求
	IsStream bool
}
