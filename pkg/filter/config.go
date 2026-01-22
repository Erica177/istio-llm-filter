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
	"mosn.io/htnn/api/pkg/filtermanager/api"
	"mosn.io/htnn/api/pkg/plugins"

	"github.com/istio-llm-filter/pkg/config"
	"github.com/istio-llm-filter/pkg/metadata"
)

func init() {
	// 注册插件
	plugins.RegisterPlugin(Name, &plugin{})
}

// plugin 实现 plugins.Plugin 接口
type plugin struct {
	plugins.PluginMethodDefaultImpl
}

// Type 返回插件类型
func (p *plugin) Type() plugins.PluginType {
	return plugins.TypeTransform
}

// Order 返回插件顺序
func (p *plugin) Order() plugins.PluginOrder {
	return plugins.PluginOrder{
		Position: plugins.OrderPositionAccess,
	}
}

// NonBlockingPhases 返回非阻塞阶段
func (p *plugin) NonBlockingPhases() api.Phase {
	return api.PhaseEncodeHeaders | api.PhaseEncodeData | api.PhaseEncodeResponse | api.PhaseEncodeTrailers
}

// Config 返回配置实例
func (p *plugin) Config() api.PluginConfig {
	return &config.LLMProxyConfig{}
}

// FilterFactory 创建过滤器实例
func FilterFactory(c interface{}, callbacks api.FilterCallbackHandler) api.Filter {
	cfg := c.(*config.LLMProxyConfig)

	// 初始化 Metadata-Center 客户端
	if cfg.MC == nil {
		cfg.MC = metadata.GetClientOrNoop()
	}

	return &Filter{
		callbacks: callbacks,
		config:    cfg,
	}
}

// ConfigParser 配置解析器
type ConfigParser struct{}

// Parse 解析配置
func (p *ConfigParser) Parse(any interface{}, callbacks api.ConfigCallbackHandler) (interface{}, error) {
	cfg := &config.LLMProxyConfig{}

	// 从 any 解析配置
	// any 可能是 map[string]interface{} 或 []byte
	switch v := any.(type) {
	case map[string]interface{}:
		// 将 map 转换为 JSON 再解析
		data, err := sonic.Marshal(v)
		if err != nil {
			return nil, err
		}
		if err := sonic.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	case []byte:
		if err := sonic.Unmarshal(v, cfg); err != nil {
			return nil, err
		}
	}

	// 初始化配置
	if err := cfg.Init(callbacks); err != nil {
		return nil, err
	}

	// 验证配置
	if err := cfg.Parse(callbacks); err != nil {
		return nil, err
	}

	api.LogInfof("LLM Proxy config parsed: protocol=%s, algorithm=%s, models=%d",
		cfg.GetProtocol(), cfg.GetAlgorithm(), len(cfg.ModelMappings))

	return cfg, nil
}

// Merge 合并配置
func (p *ConfigParser) Merge(parent interface{}, child interface{}) interface{} {
	// 子配置覆盖父配置
	if child != nil {
		return child
	}
	return parent
}
