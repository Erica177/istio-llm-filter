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

// Package transcoder 提供请求/响应协议转码功能
package transcoder

import (
	"mosn.io/htnn/api/pkg/filtermanager/api"

	"github.com/istio-llm-filter/pkg/config"
	"github.com/istio-llm-filter/pkg/types"
)

// Transcoder 定义协议转码器接口
type Transcoder interface {
	// GetRequestData 解析请求信息，提取模型名、Prompt 等
	GetRequestData(headers api.RequestHeaderMap, data []byte) (*types.RequestData, error)

	// EncodeRequest 编码请求到后端协议格式
	EncodeRequest(modelName, backendProtocol string, headers api.RequestHeaderMap, buffer api.BufferInstance) (*types.RequestContext, error)

	// DecodeHeaders 解码响应头
	DecodeHeaders(headers api.ResponseHeaderMap) error

	// GetResponseData 转码响应数据
	GetResponseData(data []byte) (output []byte, err error)

	// GetLLMLogItems 获取 LLM 日志项
	GetLLMLogItems() *types.LLMLogItems
}

// Factory 转码器工厂函数类型
type Factory func(callbacks api.FilterCallbackHandler, config *config.LLMProxyConfig) Transcoder

// 转码器工厂注册表
var factories = make(map[string]Factory)

// RegisterFactory 注册转码器工厂
func RegisterFactory(protocol string, factory Factory) {
	factories[protocol] = factory
}

// GetFactory 获取转码器工厂
func GetFactory(protocol string) Factory {
	return factories[protocol]
}
