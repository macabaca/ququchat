#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT_DIR"

ENV_FILE="${1:-./internal/config/config.secrets.env}"

docker compose --env-file "$ENV_FILE" build --no-cache ququchat-api ququchat-taskservice
docker compose --env-file "$ENV_FILE" up -d --force-recreate ququchat-api ququchat-taskservice
docker compose --env-file "$ENV_FILE" ps
