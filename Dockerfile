FROM golang:1.25 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/ququchat-api ./cmd
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/ququchat-taskservice ./cmd/taskservice

FROM debian:bookworm-slim

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/ququchat-api /app/bin/ququchat-api
COPY --from=builder /out/ququchat-taskservice /app/bin/ququchat-taskservice

EXPOSE 8080

CMD ["/app/bin/ququchat-api"]
