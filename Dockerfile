FROM golang:1.25 AS builder

WORKDIR /src

ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/ququchat-api ./cmd
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/ququchat-taskservice ./cmd/taskservice

FROM ubuntu:noble

WORKDIR /app

RUN sed -i 's|http://archive.ubuntu.com|https://mirrors.aliyun.com|g' /etc/apt/sources.list && \
    apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates tzdata && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/ququchat-api /app/bin/ququchat-api
COPY --from=builder /out/ququchat-taskservice /app/bin/ququchat-taskservice

EXPOSE 8080

CMD ["/app/bin/ququchat-api"]
