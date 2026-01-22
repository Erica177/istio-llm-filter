# Copyright The AIGW Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# =============================================================================
# istio-llm-filter Makefile
# 用于编译 Envoy Golang Filter 插件
# =============================================================================

# Go 相关配置
GO := go
GOOS ?= linux
GOARCH ?= amd64
CGO_ENABLED := 1

# 版本信息
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# 构建标志
BUILD_TAGS := so
LDFLAGS := -s -w \
	-X 'main.Version=$(VERSION)' \
	-X 'main.BuildTime=$(BUILD_TIME)' \
	-X 'main.GitCommit=$(GIT_COMMIT)'

# 目录配置
BUILD_DIR := build
CMD_DIR := cmd
OUTPUT_NAME := libllmproxy.so

# 默认目标
.PHONY: all
all: build

# =============================================================================
# 构建目标
# =============================================================================

# 构建共享库
.PHONY: build
build: $(BUILD_DIR)/$(OUTPUT_NAME)

$(BUILD_DIR)/$(OUTPUT_NAME): $(BUILD_DIR)
	@echo "Building $(OUTPUT_NAME)..."
	@echo "  GOOS=$(GOOS) GOARCH=$(GOARCH)"
	@echo "  VERSION=$(VERSION)"
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) \
		$(GO) build -tags=$(BUILD_TAGS) \
		-buildmode=c-shared \
		-ldflags="$(LDFLAGS)" \
		-o $@ \
		./$(CMD_DIR)/main.go
	@echo "Build successful: $@"

$(BUILD_DIR):
	@mkdir -p $(BUILD_DIR)

# 本地构建 (使用当前系统架构)
.PHONY: build-local
build-local:
	@$(MAKE) build GOOS=$(shell go env GOOS) GOARCH=$(shell go env GOARCH)

# ARM64 构建
.PHONY: build-arm64
build-arm64:
	@$(MAKE) build GOARCH=arm64

# =============================================================================
# 开发目标
# =============================================================================

# 下载依赖
.PHONY: deps
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download
	$(GO) mod tidy

# 代码检查
.PHONY: lint
lint:
	@echo "Running lint..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, skipping..."; \
	fi

# 格式化代码
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

# 代码审查
.PHONY: vet
vet:
	@echo "Running go vet..."
	$(GO) vet ./...

# 运行测试
.PHONY: test
test:
	@echo "Running tests..."
	$(GO) test -v -race ./...

# 运行测试并生成覆盖率报告
.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GO) test -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# 验证构建 (不输出文件，仅检查编译)
.PHONY: verify
verify:
	@echo "Verifying build..."
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build -tags=$(BUILD_TAGS) -o /dev/null ./$(CMD_DIR)/main.go
	@echo "Build verification passed"

# =============================================================================
# 清理目标
# =============================================================================

# 清理构建产物
.PHONY: clean
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

# 完全清理 (包括依赖缓存)
.PHONY: clean-all
clean-all: clean
	@echo "Cleaning Go cache..."
	$(GO) clean -cache -testcache

# =============================================================================
# Docker 构建
# =============================================================================

# Docker 镜像名称
DOCKER_IMAGE ?= istio-llm-filter
DOCKER_TAG ?= $(VERSION)

# 在 Docker 中构建 (确保 Linux 环境)
.PHONY: docker-build
docker-build:
	@echo "Building in Docker..."
	docker run --rm \
		-v $(PWD):/workspace \
		-w /workspace \
		-e GOOS=linux \
		-e GOARCH=amd64 \
		-e CGO_ENABLED=1 \
		golang:1.22 \
		make build

# =============================================================================
# 帮助
# =============================================================================

.PHONY: help
help:
	@echo "istio-llm-filter Makefile"
	@echo ""
	@echo "构建目标:"
	@echo "  build        - 构建共享库 (默认 linux/amd64)"
	@echo "  build-local  - 使用本地架构构建"
	@echo "  build-arm64  - 构建 ARM64 版本"
	@echo ""
	@echo "开发目标:"
	@echo "  deps         - 下载依赖"
	@echo "  lint         - 代码检查"
	@echo "  fmt          - 格式化代码"
	@echo "  vet          - 运行 go vet"
	@echo "  test         - 运行测试"
	@echo "  test-coverage- 生成测试覆盖率报告"
	@echo "  verify       - 验证编译通过"
	@echo ""
	@echo "清理目标:"
	@echo "  clean        - 清理构建产物"
	@echo "  clean-all    - 完全清理"
	@echo ""
	@echo "Docker 目标:"
	@echo "  docker-build - 在 Docker 中构建"
	@echo ""
	@echo "变量:"
	@echo "  GOOS         - 目标操作系统 (默认: linux)"
	@echo "  GOARCH       - 目标架构 (默认: amd64)"
	@echo "  VERSION      - 版本号 (默认: git tag)"
