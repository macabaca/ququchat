#!/bin/bash

LOG_DIR="$(cd "$(dirname "$0")" && pwd)"

docker compose logs --follow --since=0 ququchat-api > "$LOG_DIR/ququchat.log" 2>&1 &
docker compose logs --follow --since=0 ququchat-taskservice > "$LOG_DIR/taskservice.log" 2>&1 &

echo "日志已导出:"
echo "  ququchat.log     -> $LOG_DIR/ququchat.log"
echo "  taskservice.log  -> $LOG_DIR/taskservice.log"
echo "  按 Ctrl+C 停止"
tail -f "$LOG_DIR/ququchat.log" "$LOG_DIR/taskservice.log"
