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

package filter

import (
	"github.com/bytedance/sonic"
	"github.com/envoyproxy/envoy/contrib/golang/common/go/api"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/istio-llm-filter/pkg/config"
	"github.com/istio-llm-filter/pkg/metadata"
)

// ConfigParser 实现 api.StreamFilterConfigParser 接口
// 负责解析和验证配置
type ConfigParser struct{}

// NewConfigParser 创建配置解析器
func NewConfigParser() *ConfigParser {
	return &ConfigParser{}
}

// Parse 解析配置
// 实现 api.StreamFilterConfigParser 接口
// any 参数是从 Envoy 传入的 protobuf Any 类型配置
func (p *ConfigParser) Parse(any *anypb.Any, callbacks api.ConfigCallbackHandler) (interface{}, error) {
	cfg := &config.LLMProxyConfig{}

	// 从 Any 中获取配置数据
	// Envoy 会将 typed_config 中的 @type 为 type.googleapis.com/xds.type.v3.TypedStruct 的配置
	// 转换为 JSON 格式传入
	configBytes := any.GetValue()
	if len(configBytes) > 0 {
		if err := sonic.Unmarshal(configBytes, cfg); err != nil {
			return nil, err
		}
	}

	// 初始化配置
	if err := cfg.Init(); err != nil {
		return nil, err
	}

	// 验证配置
	if err := cfg.Parse(); err != nil {
		return nil, err
	}

	// 初始化 Metadata-Center 客户端
	cfg.MC = metadata.GetClientOrNoop()

	api.LogInfof("LLM Proxy config parsed: protocol=%s, algorithm=%s, models=%d",
		cfg.GetProtocol(), cfg.GetAlgorithm(), len(cfg.ModelMappings))

	return cfg, nil
}

// Merge 合并配置
// 实现 api.StreamFilterConfigParser 接口
func (p *ConfigParser) Merge(parent interface{}, child interface{}) interface{} {
	// 子配置覆盖父配置
	if child != nil {
		return child
	}
	return parent
}

// Factory 创建过滤器实例
// 实现 api.StreamFilterFactory 类型
func Factory(c interface{}, callbacks api.FilterCallbackHandler) api.StreamFilter {
	cfg := c.(*config.LLMProxyConfig)
	return &Filter{
		callbacks: callbacks,
		config:    cfg,
	}
}
