# Istio LLM Filter 使用说明

本文档介绍如何编译、部署和使用 Istio LLM Filter。

## 技术基础

本插件基于 [Envoy Golang Filter](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/golang_filter) 开发，直接使用 Envoy 原生的 Golang Filter API。

**核心依赖**：
- `github.com/envoyproxy/envoy/contrib/golang/common/go/api` - Filter 接口定义
- `github.com/envoyproxy/envoy/contrib/golang/filters/http/source/go/pkg/http` - Filter 注册

## 编译

### 前置要求

- Go 1.22+
- Make
- CGO 支持（编译 .so 共享库）

### 编译命令

```bash
cd istio-llm-filter

# Linux AMD64（推荐用于生产部署）
make build

# 本地架构（用于开发测试）
make build-local

# ARM64
make build-arm64

# 仅验证编译（不输出文件）
make verify
```

编译完成后在 `build/` 目录生成 `libllmproxy.so` 文件。

**注意**：生产环境部署时，需要使用与 Envoy 相同的 Go 版本和 glibc 版本编译，建议使用 Envoy 官方的 Bazel Go SDK 进行编译。

## 部署

### 1. 部署 Metadata-Center（可选）

如果需要启用负载感知和缓存感知路由，需要部署 Metadata-Center 服务：

```yaml
apiVersion: v1
kind: Service
metadata:
  name: metadata-center
  namespace: llm
spec:
  ports:
    - port: 80
      targetPort: 8080
  selector:
    app: metadata-center
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: metadata-center
  namespace: llm
spec:
  replicas: 1
  selector:
    matchLabels:
      app: metadata-center
  template:
    metadata:
      labels:
        app: metadata-center
    spec:
      containers:
        - name: metadata-center
          image: your-registry/metadata-center:latest
          ports:
            - containerPort: 8080
```

### 2. 挂载 Filter 到 Istio Gateway

将编译好的 `.so` 文件挂载到 Istio Ingress Gateway：

**方式一：使用 ConfigMap（适合小文件）**

```bash
# 创建 ConfigMap
kubectl create configmap llm-filter-so \
  --from-file=libllmproxy.so=build/libllmproxy.so \
  -n istio-system
```

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: istio-ingressgateway
  namespace: istio-system
spec:
  template:
    spec:
      containers:
        - name: istio-proxy
          env:
            # Metadata-Center 配置
            - name: METADATA_CENTER_HOST
              value: "metadata-center.llm.svc.cluster.local"
            - name: METADATA_CENTER_PORT
              value: "80"
            - name: METADATA_CENTER_ENABLED
              value: "true"
            - name: METADATA_CENTER_CACHE_ENABLED
              value: "true"
          volumeMounts:
            - name: llm-filter
              mountPath: /etc/envoy/libllmproxy.so
              subPath: libllmproxy.so
      volumes:
        - name: llm-filter
          configMap:
            name: llm-filter-so
```

**方式二：使用 PersistentVolume（适合大文件）**

```yaml
volumes:
  - name: llm-filter
    persistentVolumeClaim:
      claimName: llm-filter-pvc
```

**方式三：自定义镜像（推荐生产环境）**

```dockerfile
FROM docker.io/istio/proxyv2:1.20.0
COPY libllmproxy.so /etc/envoy/libllmproxy.so
```

### 3. 应用 EnvoyFilter

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

完整配置示例参见 [examples/envoyfilter.yaml](../examples/envoyfilter.yaml)。

## 环境变量配置

通过环境变量配置 Metadata-Center 连接参数：

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `METADATA_CENTER_HOST` | localhost | Metadata-Center 服务地址 |
| `METADATA_CENTER_PORT` | 8080 | Metadata-Center 服务端口 |
| `METADATA_CENTER_ENABLED` | false | 是否启用负载感知（总开关） |
| `METADATA_CENTER_CACHE_ENABLED` | false | 是否启用缓存感知 |
| `METADATA_CENTER_TIMEOUT_MS` | 100 | 同步查询超时时间（毫秒） |
| `METADATA_CENTER_ASYNC_TIMEOUT_MS` | 500 | 异步更新超时时间（毫秒） |
| `METADATA_CENTER_ASYNC_QUEUE_SIZE` | 1000 | 异步任务队列大小 |
| `METADATA_CENTER_ASYNC_WORKERS` | 10 | 异步工作协程数量 |

## 配置参数说明

### EnvoyFilter typed_config 字段

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `library_id` | string | 是 | 库标识符，用于 Envoy 内部缓存 |
| `library_path` | string | 是 | .so 共享库的绝对路径 |
| `plugin_name` | string | 是 | 插件名称，必须与 Go 代码中注册的名称一致 |
| `plugin_config` | TypedStruct | 是 | 插件配置 |

### plugin_config.value 字段

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `protocol` | string | 是 | 输入协议类型，目前支持 `openai` |
| `algorithm` | string | 否 | 负载均衡算法，默认 `inference_lb` |
| `model_mapping_rule` | map | 是 | 模型到路由规则的映射 |
| `lb_mapping_rule` | map | 否 | 模型到负载均衡配置的映射 |

### model_mapping_rule

模型路由规则，将客户端请求的模型名映射到后端集群：

```yaml
model_mapping_rule:
  <model_name>:          # 客户端请求的模型名
    rules:
      - scene_name: xxx  # 场景名称（用于日志）
        cluster: xxx     # 后端集群名（Istio 格式: outbound|port||hostname）
        backend: vllm    # 后端类型: vllm, sglang, triton
        headers:         # 请求头匹配条件（可选，用于条件路由）
          - key: x-env
            value: prod
        subset:          # 后端子集过滤（可选）
          - name: main
            labels:
              version: v1
            lora: lora-adapter-1  # LoRA 适配器名称（可选）
```

### lb_mapping_rule

负载均衡配置：

```yaml
lb_mapping_rule:
  <model_name>:
    load_aware_enable: true    # 启用负载感知（需要 Metadata-Center）
    cache_aware_enable: true   # 启用缓存感知（需要 Metadata-Center）
    candidate_percent: 10      # Top N% 候选比例 (1-100)
    request_load_weight: 1     # W2: 请求队列权重
    prefill_load_weight: 3     # W3: Prefill 队列权重
    cache_radio_weight: 2      # W1: 缓存命中权重
```

**评分公式**：`Score = W1 × CacheRatio - W2 × NormReqLoad - W3 × NormPrefillLoad`

## 测试

### 发送测试请求

```bash
# 非流式请求
curl -X POST http://gateway:10000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen-2.5-72b",
    "messages": [
      {"role": "user", "content": "Hello, how are you?"}
    ]
  }'

# 流式请求
curl -X POST http://gateway:10000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen-2.5-72b",
    "messages": [
      {"role": "user", "content": "Hello, how are you?"}
    ],
    "stream": true
  }'
```

### 查看日志

```bash
# 查看 Gateway 日志
kubectl logs -n istio-system -l istio=ingressgateway -f | grep "llm-proxy"

# 查看选择的后端
kubectl logs -n istio-system -l istio=ingressgateway | grep "selected backend"
```

## 故障排查

### 1. 插件未加载

**症状**：请求直接透传，没有负载均衡效果

**排查步骤**：

```bash
# 检查 .so 文件是否正确挂载
kubectl exec -n istio-system deploy/istio-ingressgateway -- ls -la /etc/envoy/libllmproxy.so

# 检查 EnvoyFilter 是否生效
istioctl proxy-config listener deploy/istio-ingressgateway -n istio-system -o json | grep golang
```

### 2. Metadata-Center 连接失败

**症状**：日志中显示 "query load failed" 或 "query kv cache failed"

**排查步骤**：

```bash
# 检查网络连通性
kubectl exec -n istio-system deploy/istio-ingressgateway -- \
  curl -v http://metadata-center.llm.svc.cluster.local:80/health

# 检查环境变量
kubectl exec -n istio-system deploy/istio-ingressgateway -- env | grep METADATA
```

### 3. 模型未找到

**症状**：返回 404 错误，日志显示 "model xxx not found in mapping rules"

**排查步骤**：
- 检查 `model_mapping_rule` 配置中的模型名是否与请求中的 `model` 字段一致
- 模型名区分大小写

### 4. 负载均衡不生效

**症状**：所有请求都路由到同一个 Pod

**排查步骤**：
- 确认 `load_aware_enable` 和 `cache_aware_enable` 设置为 `true`
- 确认 `METADATA_CENTER_ENABLED` 环境变量为 `true`
- 检查 Metadata-Center 是否返回了有效的负载数据
- 查看日志中的评分计算详情

### 5. 编译错误

**症状**：`go build` 报错

**常见原因**：
- Go 版本不匹配：需要 Go 1.22+
- CGO 未启用：确保 `CGO_ENABLED=1`
- 依赖版本冲突：运行 `go mod tidy` 更新依赖

## 版本兼容性

| 组件 | 版本要求 |
|------|----------|
| Go | 1.22+ |
| Envoy | 1.32.x |
| Istio | 1.20+ |
| envoyproxy/envoy Go SDK | v1.32.0 |

**重要**：Go 插件 SDK 版本必须与 Envoy 版本匹配，否则可能导致运行时错误。

## 参考文档

- [Envoy Golang Filter 官方文档](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/golang_filter)
- [Envoy Golang Filter Proto 定义](https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/filters/http/golang/v3alpha/golang.proto)
- [Istio EnvoyFilter 配置](https://istio.io/latest/docs/reference/config/networking/envoy-filter/)
