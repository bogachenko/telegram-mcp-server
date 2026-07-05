# Deploy

This service is intended for manual start, not autostart.

Do not run:

```bash
sudo systemctl enable telegram-mcp-server
```

Run only when needed:

```bash
sudo systemctl start telegram-mcp-server
sleep 2
sudo systemctl status telegram-mcp-server --no-pager -l
```

Stop:

```bash
sudo systemctl stop telegram-mcp-server
```

## Env file

Create the service env file:

```bash
sudo cp .env.example /etc/telegram-mcp-server.env
sudo nano /etc/telegram-mcp-server.env
sudo chmod 600 /etc/telegram-mcp-server.env
```

Required values:

```bash
TGMCP_TELEGRAM_API_ID=...
TGMCP_TELEGRAM_API_HASH=...
TGMCP_TELEGRAM_PHONE=...

TGMCP_LISTEN_ADDR=127.0.0.1:1984
TGMCP_PUBLIC_BASE_URL=https://tg-mcp.elektrosila-avtomatika.store

TGMCP_OAUTH_OWNER_TOKEN=some-long-random-owner-token
```

Generate owner token:

```bash
openssl rand -base64 48
```

`TGMCP_BEARER_TOKEN` is not needed for this setup. OAuth uses `TGMCP_OAUTH_OWNER_TOKEN`.

## Build binary

From repo root:

```bash
go test ./...
go vet ./...
go build -o /tmp/telegram-mcp-server ./cmd/telegram-mcp
sudo install -m 0755 /tmp/telegram-mcp-server /usr/local/bin/telegram-mcp-server
rm -f /tmp/telegram-mcp-server
```

## Telegram session

Create the Telegram MTProto session before starting the service.

Because `systemd` uses `/etc/telegram-mcp-server.env`, use the same env file for the auth command:

```bash
set -a
source /etc/telegram-mcp-server.env
set +a

telegram-mcp-server telegram-auth
telegram-mcp-server telegram-me
```

The session file is runtime data:

```text
data/session/session.json
```

It must not be committed.

## Install systemd service

```bash
sudo cp deploy/telegram-mcp-server.service.example /etc/systemd/system/telegram-mcp-server.service
sudo systemctl daemon-reload
```

Manual start:

```bash
sudo systemctl start telegram-mcp-server
sleep 2
sudo systemctl status telegram-mcp-server --no-pager -l
```

Logs:

```bash
journalctl -u telegram-mcp-server -f
```

Healthcheck:

```bash
curl http://127.0.0.1:1984/healthz
```

Expected:

```text
ok
```

## Nginx

Use `deploy/nginx.telegram-mcp.conf.example` inside the HTTPS server block for:

```text
tg-mcp.elektrosila-avtomatika.store
```

## Public MCP endpoint

```text
https://tg-mcp.elektrosila-avtomatika.store/mcp
```

OAuth metadata:

```bash
curl https://tg-mcp.elektrosila-avtomatika.store/.well-known/oauth-protected-resource
curl https://tg-mcp.elektrosila-avtomatika.store/.well-known/oauth-authorization-server
```
