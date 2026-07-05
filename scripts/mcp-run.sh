#!/usr/bin/env bash
set -euo pipefail

cd /home/i-b8o/src/github.com/bogachenko/telegram-mcp-server

if [ -f "$HOME/.config/telegram-mcp-server/env" ]; then
  . "$HOME/.config/telegram-mcp-server/env"
fi

export TGMCP_PUBLIC_BASE_URL="https://tg-mcp.elektrosila-avtomatika.store"
export TGMCP_OAUTH_OWNER_TOKEN="$(tr -d '\n\r' < "$HOME/.config/telegram-mcp-server/oauth-owner-token")"
export TGMCP_OAUTH_STATE_FILE="$HOME/.config/telegram-mcp-server/oauth-state.json"

echo "TGMCP_PUBLIC_BASE_URL=$TGMCP_PUBLIC_BASE_URL"
echo "TGMCP_OAUTH_OWNER_TOKEN len=${#TGMCP_OAUTH_OWNER_TOKEN}"
echo "TGMCP_OAUTH_STATE_FILE=$TGMCP_OAUTH_STATE_FILE"

exec go run ./cmd/telegram-mcp serve \
  --listen-addr 127.0.0.1:1984
