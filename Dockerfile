# 构建阶段 - 使用 Go 1.26
FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

# 复制所有源代码和内容
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o server .

# 运行阶段
FROM alpine:latest

# 显式声明生产环境：CORS 的"放行本地来源"规则只在 ENV=development 时生效，
# 这里写死 production，避免有人把本地 .env 打进镜像后意外继承开发行为。
ENV ENV=production

WORKDIR /root/

# 复制二进制文件
COPY --from=builder /app/server .

# 复制 content 目录到镜像中
COPY --from=builder /app/content ./content

EXPOSE 8080

CMD ["./server"]