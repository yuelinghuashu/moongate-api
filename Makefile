# moongate-api 开发命令入口
#
# 常用命令：
#   make test     - 运行全部测试（详细输出）
#   make check    - 代码检查 + 全部测试（同 CI）
#   make run      - 本地开发运行
#   make build    - 编译二进制

.PHONY: help test test-short vet build run check docker-build clean

help: ## 显示所有可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

test: ## 运行全部测试（详细输出）
	go test ./... -v

test-short: ## 快速运行全部测试
	go test ./...

vet: ## 运行静态代码检查
	go vet ./...

build: ## 编译二进制到当前目录
	go build -o moongate-api .

run: ## 本地开发运行（需 .env）
	go run main.go

check: vet test-short ## 代码检查 + 全部测试（与 CI 一致）

docker-build: ## 构建 Docker 镜像
	docker build -t moongate-api .

clean: ## 清理编译产物
	rm -f moongate-api