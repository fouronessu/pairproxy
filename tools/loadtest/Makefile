# Claude Load Tester Makefile

# 变量
BINARY_NAME := claude-load-tester
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# 平台
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

# Go 参数
GO := go
GOFLAGS := -v

# 目录
CMD_DIR := ./cmd
BUILD_DIR := ./build
DIST_DIR := ./dist

.PHONY: all build clean test fmt lint docker-build docker-push release install help

# 默认目标
all: build

# 帮助
help:
	@echo "Claude Load Tester - Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build          Build for current platform"
	@echo "  make build-all      Build for all platforms"
	@echo "  make test           Run tests"
	@echo "  make clean          Clean build artifacts"
	@echo "  make docker-build   Build Docker image"
	@echo "  make docker-push    Push Docker image"
	@echo "  make release        Create release builds"
	@echo "  make install        Install to GOPATH/bin"
	@echo "  make fmt            Format code"
	@echo "  make lint           Run linter"
	@echo "  make deps           Download dependencies"
	@echo "  make run            Run with default settings"
	@echo ""

# 构建当前平台
build:
	@mkdir -p $(BUILD_DIR)
	@echo "Building $(BINARY_NAME) $(VERSION)..."
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# 构建所有平台
build-all: $(PLATFORMS)

$(PLATFORMS):
	@mkdir -p $(BUILD_DIR)
	@GOOS=$(word 1,$(subst /, ,$@)) GOARCH=$(word 2,$(subst /, ,$@)) \
		$(GO) build $(GOFLAGS) $(LDFLAGS) \
		-o $(BUILD_DIR)/$(BINARY_NAME)-$(word 1,$(subst /, ,$@))-$(word 2,$(subst /, ,$@))$(if $(findstring windows,$(word 1,$(subst /, ,$@))),.exe,) \
		$(CMD_DIR)
	@echo "Built for $@"

# 清理
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR) $(DIST_DIR)
	@$(GO) clean
	@echo "Clean complete"

# 测试
test:
	@echo "Running tests..."
	$(GO) test -v -race ./...
	@echo "Tests complete"

# 短测试
test-short:
	@echo "Running short tests..."
	$(GO) test -v -short ./...

# 格式化
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...
	@echo "Format complete"

# 代码检查
lint:
	@echo "Running linter..."
	@if command -v golangci-lint > /dev/null; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, installing..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
		golangci-lint run; \
	fi

# 下载依赖
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download
	$(GO) mod tidy
	@echo "Dependencies downloaded"

# 更新依赖
deps-update:
	@echo "Updating dependencies..."
	$(GO) get -u ./...
	$(GO) mod tidy
	@echo "Dependencies updated"

# 安装到 GOPATH/bin
install:
	@echo "Installing $(BINARY_NAME)..."
	$(GO) install $(LDFLAGS) $(CMD_DIR)
	@echo "Installed to $(shell $(GO) env GOPATH)/bin/$(BINARY_NAME)"

# 运行（开发模式）
run: build
	./$(BUILD_DIR)/$(BINARY_NAME) run --mode ramp-up --initial 1 --max 10 --output ./results/dev-test.json

# 运行 API 模式
run-api: build
	./$(BUILD_DIR)/$(BINARY_NAME) run --mode fixed --workers 10 --api-enabled --api-addr ":8080"

# Docker 构建
docker-build:
	@echo "Building Docker image..."
	docker build -t $(BINARY_NAME):$(VERSION) -t $(BINARY_NAME):latest .
	@echo "Docker build complete"

# Docker 推送
docker-push: docker-build
	@echo "Pushing Docker image..."
	docker push $(BINARY_NAME):$(VERSION)
	docker push $(BINARY_NAME):latest
	@echo "Docker push complete"

# 创建发布包
release: clean build-all
	@mkdir -p $(DIST_DIR)
	@echo "Creating release packages..."
	@for platform in $(PLATFORMS); do \
		GOOS=$$(echo $$platform | cut -d'/' -f1); \
		GOARCH=$$(echo $$platform | cut -d'/' -f2); \
		BINARY=$(BINARY_NAME)-$$GOOS-$$GOARCH; \
		EXT=""; \
		if [ "$$GOOS" = "windows" ]; then EXT=".exe"; fi; \
		\
		mkdir -p $(DIST_DIR)/$(BINARY_NAME)-$$GOOS-$$GOARCH; \
		cp $(BUILD_DIR)/$$BINARY$$EXT $(DIST_DIR)/$(BINARY_NAME)-$$GOOS-$$GOARCH/$(BINARY_NAME)$$EXT; \
		cp README.md $(DIST_DIR)/$(BINARY_NAME)-$$GOOS-$$GOARCH/; \
		cp MANUAL.md $(DIST_DIR)/$(BINARY_NAME)-$$GOOS-$$GOARCH/; \
		cp LICENSE $(DIST_DIR)/$(BINARY_NAME)-$$GOOS-$$GOARCH/ 2>/dev/null || true; \
		cp -r prompts $(DIST_DIR)/$(BINARY_NAME)-$$GOOS-$$GOARCH/; \
		cp -r config $(DIST_DIR)/$(BINARY_NAME)-$$GOOS-$$GOARCH/; \
		\
		cd $(DIST_DIR) && tar czf $(BINARY_NAME)-$$GOOS-$$GOARCH.tar.gz $(BINARY_NAME)-$$GOOS-$$GOARCH; \
		cd $(DIST_DIR) && zip -r $(BINARY_NAME)-$$GOOS-$$GOARCH.zip $(BINARY_NAME)-$$GOOS-$$GOARCH; \
		\
		echo "Created release package for $$platform"; \
	done
	@echo "Release packages created in $(DIST_DIR)/"

# 基准测试
bench:
	@echo "Running benchmarks..."
	$(GO) test -bench=. -benchmem ./...

# 覆盖率
coverage:
	@echo "Running coverage..."
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# 代码生成（如果有）
generate:
	$(GO) generate ./...

# 检查
.PHONY: check
check: fmt lint test
	@echo "All checks passed!"

# CI/CD 相关
ci-build: deps build test

ci-release: clean release

# 本地开发环境设置
dev-setup:
	@echo "Setting up development environment..."
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	$(GO) install github.com/goreleaser/goreleaser@latest
	@echo "Development environment setup complete"

# 打印版本
version:
	@echo "Version: $(VERSION)"
	@echo "Build Time: $(BUILD_TIME)"

# 检查 Go 版本
.PHONY: check-go
check-go:
	@$(GO) version
	@if [ $(shell $(GO) version | grep -o 'go[0-9]\+.[0-9]\+' | sed 's/go//' | cut -d. -f1) -lt 1 ]; then \
		echo "Error: Go 1.21+ required"; \
		exit 1; \
	fi
