# 构建阶段
FROM golang:1.26-alpine AS builder

WORKDIR /app

# 复制依赖文件
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码并编译
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server .

# 运行阶段
FROM alpine:latest

WORKDIR /root/

# 复制二进制文件
COPY --from=builder /app/server .

EXPOSE 8080

CMD ["./server"]