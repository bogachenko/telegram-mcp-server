# Deploy

Production defaults used by this repo:

```bash
TGMCP_LISTEN_ADDR=127.0.0.1:1984
TGMCP_PUBLIC_BASE_URL=https://tg-mcp.elektrosila-avtomatika.store
```

These are non-secret values and are set directly in `deploy/telegram-mcp-server.service.example`.

## Secrets

Do not put real secrets into git.

For interactive CLI usage, exporting Telegram credentials from `~/.bashrc` is fine:

```bash
export TGMCP_TELEGRAM_API_ID=...
export TGMCP_TELEGRAM_API_HASH=...
export TGMCP_TELEGRAM_PHONE=...
```

`systemd` does not read `~/.bashrc`, so the service uses a separate env file in the user's home directory:

```bash
mkdir -p ~/.config/telegram-mcp-server
chmod 700 ~/.config/telegram-mcp-server

cp .env.example ~/.config/telegram-mcp-server/env
nano ~/.config/telegram-mcp-server/env
chmod 600 ~/.config/telegram-mcp-server/env
```

Minimum file content:

```bash
TGMCP_TELEGRAM_API_ID=...
TGMCP_TELEGRAM_API_HASH=...
TGMCP_TELEGRAM_PHONE=...
TGMCP_BEARER_TOKEN=some-long-random-token
```

OAuth owner-token mode is also supported:

```bash
TGMCP_OAUTH_OWNER_TOKEN=some-long-random-owner-token
```

You may use both `TGMCP_BEARER_TOKEN` and `TGMCP_OAUTH_OWNER_TOKEN`.

## Auth modes

### Static bearer token

Set:

```bash
TGMCP_BEARER_TOKEN=some-long-random-token
```

Then MCP clients must call `/mcp` with:

```text
Authorization: Bearer some-long-random-token
```

### OAuth authorization-code flow

Set:

```bash
TGMCP_OAUTH_OWNER_TOKEN=some-long-random-owner-token
```

The server exposes:

```text
/.well-known/oauth-protected-resource
/.well-known/oauth-authorization-server
/oauth/register
/oauth/authorize
/oauth/token
```

During `/oauth/authorize`, enter the owner token. The server then issues a Bearer access token for the MCP client.

## Build binary

From repo root:

```bash
go test ./...
go vet ./...
go build -o /tmp/telegram-mcp-server ./cmd/telegram-mcp

sudo install -m 0755 /tmp/telegram-mcp-server /usr/local/bin/telegram-mcp-server
rm -f /tmp/telegram-mcp-server
```

Check config:

```bash
telegram-mcp-server status
```

## Telegram session

Create the Telegram MTProto session on the target server:

```bash
source ~/.bashrc
telegram-mcp-server telegram-auth
telegram-mcp-server telegram-me
```

The session file is local runtime data:

```text
data/session/session.json
```

It must not be committed or copied into git.

## systemd

Install the service:

```bash
sudo cp deploy/telegram-mcp-server.service.example /etc/systemd/system/telegram-mcp-server.service
sudo systemctl daemon-reload
sudo systemctl enable telegram-mcp-server
sudo systemctl start telegram-mcp-server
```

Check status/logs:

```bash
systemctl status telegram-mcp-server --no-pager
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

The proxy must pass forwarded host/proto headers because OAuth metadata uses them when `TGMCP_PUBLIC_BASE_URL` is not explicitly set.

## Public MCP endpoint

```text
https://tg-mcp.elektrosila-avtomatika.store/mcp
```

Metadata:

```bash
curl https://tg-mcp.elektrosila-avtomatika.store/.well-known/oauth-protected-resource
curl https://tg-mcp.elektrosila-avtomatika.store/.well-known/oauth-authorization-server
```
