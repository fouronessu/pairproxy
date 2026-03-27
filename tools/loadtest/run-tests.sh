#!/bin/bash
# run-tests.sh - 运行所有测试的脚本

set -e

echo "========================================"
echo "Claude Load Tester - Test Suite"
echo "========================================"
echo ""

# 进入项目目录
cd "$(dirname "$0")"

echo "Step 1: 下载依赖..."
go mod tidy

echo ""
echo "Step 2: 运行单元测试..."
go test -v ./internal/prompts/... ./internal/worker/... ./internal/metrics/... ./internal/controller/... ./cmd/...

echo ""
echo "Step 3: 运行单元测试（带覆盖率）..."
go test -cover ./internal/prompts ./internal/worker ./internal/metrics ./internal/controller ./cmd

echo ""
echo "Step 4: 构建二进制文件（用于集成测试）..."
go build -o claude-load-tester ./cmd

echo ""
echo "Step 5: 运行集成测试..."
go test -v -tags=integration ./test/integration/...

echo ""
echo "Step 6: 生成覆盖率报告..."
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | tail -1
go tool cover -html=coverage.out -o coverage.html

echo ""
echo "========================================"
echo "测试完成！"
echo ""
echo "覆盖率报告: coverage.html"
echo "========================================"
