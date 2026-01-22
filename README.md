# istio-llm-filter

基于 Envoy Golang Filter 的 LLM 推理负载均衡插件，为 Istio 提供智能的大模型请求调度能力。

## 技术基础

本插件直接使用 [Envoy Golang Filter](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/golang_filter) 原生 API 开发：

- `github.com/envoyproxy/envoy/contrib/golang/common/go/api` - Filter 接口定义
- `github.com/envoyproxy/envoy/contrib/golang/filters/http/source/go/pkg/http` - Filter 注册

## 功能特性

- **多维度负载均衡**: 基于请求负载、Prefill 队列深度、KV-Cache 命中率的综合评分算法
- **KV-Cache 感知路由**: 通过 Prompt 哈希实现缓存亲和性，提升推理效率
- **协议转码**: 支持 OpenAI 协议，兼容 vLLM、SGLang、TensorRT 等后端
- **异步元数据同步**: 非阻塞的 Metadata-Center 交互，不影响请求延迟
- **灵活配置**: 支持多模型、多环境的路由规则配置

## 快速开始

### 依赖

- Go 1.22+
- Envoy 1.32+ (with Golang Filter support)
- Metadata-Center 服务（可选，启用负载感知和缓存感知需要）

### 编译

```bash
# 编译 .so 共享库 (Linux AMD64)
make build

# 本地架构编译（开发测试用）
make build-local

# 验证编译
make verify
```

编译产物位于 `build/libllmproxy.so`。

### 部署

1. 将 `libllmproxy.so` 挂载到 Istio Gateway 容器
2. 配置 EnvoyFilter 加载插件
3. （可选）部署 Metadata-Center 服务

详细配置请参考 [使用指南](docs/usage.md)。

## 项目结构

```
istio-llm-filter/
├── cmd/                      # 插件入口
│   └── main.go
├── pkg/
│   ├── types/                # 公共类型定义
│   ├── config/               # 配置解析
│   ├── hash/                 # Prompt 哈希计算
│   ├── metadata/             # Metadata-Center 客户端
│   ├── loadbalancer/         # 负载均衡算法
│   ├── transcoder/           # 协议转码器
│   │   └── openai/           # OpenAI 协议实现
│   └── filter/               # 核心过滤器
├── docs/                     # 文档
│   ├── design.md             # 设计文档
│   ├── flowchart.md          # 执行流程图
│   └── usage.md              # 使用指南
├── examples/                 # 示例配置
│   └── envoyfilter.yaml
├── Makefile
└── go.mod
```

## 文档

- [设计文档](docs/design.md) - 架构设计和模块说明
- [执行流程图](docs/flowchart.md) - 请求处理和负载均衡流程
- [使用指南](docs/usage.md) - 部署配置和环境变量说明

## 负载均衡算法

插件实现了基于推理负载的多维度评分算法：

```
Score = W1 × CacheRatio - W2 × NormalizedRequestLoad - W3 × NormalizedPrefillLoad
```

其中：
- `CacheRatio`: KV-Cache 命中率 (0-1)
- `NormalizedRequestLoad`: 归一化的并发请求数
- `NormalizedPrefillLoad`: 归一化的 Prefill 队列深度
- `W1, W2, W3`: 可配置的权重参数

默认权重: W1=2, W2=1, W3=3（优先避免 Prefill 队列过载）

## 配置示例

```yaml
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: llm-proxy-filter
  namespace: istio-system
spec:
  workloadSelector:
    labels:
      istio: ingressgateway
  configPatches:
    - applyTo: HTTP_FILTER
      match:
        context: GATEWAY
        listener:
          filterChain:
            filter:
              name: "envoy.filters.network.http_connection_manager"
              subFilter:
                name: "envoy.filters.http.router"
      patch:
        operation: INSERT_BEFORE
        value:
          name: envoy.filters.http.golang
          typed_config:
            "@type": "type.googleapis.com/envoy.extensions.filters.http.golang.v3alpha.Config"
            library_id: llm-proxy
            library_path: /etc/envoy/libllmproxy.so
            plugin_name: llm-proxy
            plugin_config:
              "@type": "type.googleapis.com/xds.type.v3.TypedStruct"
              value:
                protocol: openai
                algorithm: inference_lb
                model_mapping_rule:
                  qwen-2.5-72b:
                    rules:
                      - scene_name: qwen-2.5-72b
                        cluster: "outbound|8000||qwen-service.llm.svc.cluster.local"
                        backend: vllm
                lb_mapping_rule:
                  qwen-2.5-72b:
                    load_aware_enable: true
                    cache_aware_enable: true
                    candidate_percent: 10
                    request_load_weight: 1
                    prefill_load_weight: 3
                    cache_radio_weight: 2
```

完整配置示例参见 [examples/envoyfilter.yaml](examples/envoyfilter.yaml)。

## 环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `METADATA_CENTER_HOST` | Metadata-Center 地址 | localhost |
| `METADATA_CENTER_PORT` | Metadata-Center 端口 | 8080 |
| `METADATA_CENTER_ENABLED` | 是否启用负载感知 | false |
| `METADATA_CENTER_CACHE_ENABLED` | 是否启用缓存感知 | false |
| `METADATA_CENTER_TIMEOUT_MS` | 同步查询超时 (ms) | 100 |

## 开发

```bash
# 格式化代码
make fmt

# 代码检查
make lint
make vet

# 运行测试
make test

# 清理构建产物
make clean
```

## 版本兼容性

| 组件 | 版本 |
|------|------|
| Go | 1.22+ |
| Envoy | 1.32.x |
| Istio | 1.20+ |

**重要**: Go 插件 SDK 版本必须与 Envoy 版本匹配。

## 参考文档

- [Envoy Golang Filter 文档](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/golang_filter)
- [Envoy Golang Filter Proto](https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/filters/http/golang/v3alpha/golang.proto)
- [Istio EnvoyFilter 配置](https://istio.io/latest/docs/reference/config/networking/envoy-filter/)

## License

Apache License 2.0
