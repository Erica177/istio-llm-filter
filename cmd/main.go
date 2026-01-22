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

//go:build so

// Package main 是 Golang Filter 插件的入口
// 编译为 .so 共享库供 Envoy 加载
package main

import (
	"github.com/envoyproxy/envoy/contrib/golang/filters/http/source/go/pkg/http"

	// 导入以注册相关组件
	"github.com/istio-llm-filter/pkg/filter"
	_ "github.com/istio-llm-filter/pkg/loadbalancer"
	_ "github.com/istio-llm-filter/pkg/metadata"
	_ "github.com/istio-llm-filter/pkg/transcoder/openai"
)

func init() {
	// 直接注册 LLM Proxy Filter
	// 使用 Envoy 原生 API 进行注册
	http.RegisterHttpFilterFactoryAndConfigParser(
		filter.Name,
		filter.Factory,
		filter.NewConfigParser(),
	)
}

func main() {
	// 共享库入口，不需要实际执行
}
