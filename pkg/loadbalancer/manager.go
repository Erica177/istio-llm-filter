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

// Package loadbalancer 实现 LLM 推理负载均衡算法
package loadbalancer

import (
	"context"
	"fmt"

	"github.com/istio-llm-filter/pkg/types"
)

// 负载均衡器工厂映射
var lbFactories = make(map[types.LoadBalancerType]types.LoadBalancerFactory)

// RegisterLbType 注册负载均衡器类型
func RegisterLbType(lbType types.LoadBalancerType, factory types.LoadBalancerFactory) {
	lbFactories[lbType] = factory
}

// CreateLbByType 根据类型创建负载均衡器
func CreateLbByType(lbType types.LoadBalancerType, ctx context.Context, hosts []types.Host) types.LoadBalancer {
	if factory, ok := lbFactories[lbType]; ok {
		return factory(ctx, hosts)
	}
	// 默认使用推理负载均衡
	if factory, ok := lbFactories[types.InferenceLB]; ok {
		return factory(ctx, hosts)
	}
	return nil
}

// ChooseServer 从集群中选择一个后端服务器
// 这是负载均衡的主入口函数
func ChooseServer(ctx context.Context, cluster string, lbType types.LoadBalancerType, hosts []types.Host) (types.Host, error) {
	if len(hosts) == 0 {
		return nil, fmt.Errorf("no available hosts in cluster %s", cluster)
	}

	// 设置集群名称到 context
	ctx = context.WithValue(ctx, types.KeyClusterName, cluster)

	// 创建负载均衡器
	lb := CreateLbByType(lbType, ctx, hosts)
	if lb == nil {
		return nil, fmt.Errorf("failed to create load balancer for type %s", lbType)
	}

	// 选择主机
	host := lb.ChooseHost(ctx)
	if host == nil {
		return nil, fmt.Errorf("failed to choose host from cluster %s", cluster)
	}

	return host, nil
}
