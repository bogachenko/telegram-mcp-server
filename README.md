# telegram-mcp-server

Telegram MCP server for reading configured Telegram channels/groups and exposing
messages to MCP clients.

## Scope

This project is a Go rewrite of the useful behavior from
`kit/telega/messages_from_multiple.chans.py`, but with a different product shape:

- Go MCP server, not an autonomous agent.
- Telegram MTProto/user-client source access, not Bot API `getUpdates`.
- SQLite-backed state and message storage.
- MCP tools/resources for sources, messages, sync, and spam/exclusions.

## MVP behavior

- Configure Telegram sources: channels and groups.
- Sync new posts/messages after the last stored message id.
- Support linked discussion chats for channel comments.
- Store normalized messages.
- Maintain per-source state.
- Maintain spam/excluded senders.
- Skip excluded senders during future scans.
- Store the evidence message that caused a sender to be excluded.

## First runtime target

```bash
go run ./cmd/telegram-mcp status
```

## Planned MCP tools

- `telegram.sources_list`
- `telegram.sources_add`
- `telegram.sources_remove`
- `telegram.sync`
- `telegram.messages_recent`
- `telegram.messages_search`
- `telegram.message_get`
- `telegram.spam_add_sender`
- `telegram.spam_add_from_message`
- `telegram.spam_remove_sender`
- `telegram.spam_list_senders`

## Telegram MTProto auth

Create a Telegram app at `my.telegram.org` and set:

```bash
export TGMCP_TELEGRAM_API_ID="123456"
export TGMCP_TELEGRAM_API_HASH="your_app_hash"
export TGMCP_TELEGRAM_PHONE="+10000000000"
```

If the Telegram account has two-step verification enabled, also set:

```bash
export TGMCP_TELEGRAM_PASSWORD="your_2fa_password"
```

Authorize the local user-client session:

```bash
go run ./cmd/telegram-mcp telegram-auth
```

Check saved session:

```bash
go run ./cmd/telegram-mcp telegram-me
```

The default session file is:

```text
data/session/session.json
```

## Source dry-run

Add a source:

```bash
go run ./cmd/telegram-mcp source-add \
  --id sellerproof_support \
  --type channel \
  --entity sellerproof_support \
  --title "SellerProof Support"
```

List sources:

```bash
go run ./cmd/telegram-mcp source-list
```

Resolve sources and preview recent messages without saving them:

```bash
go run ./cmd/telegram-mcp telegram-dry-run --limit 5
```

Preview one source:

```bash
go run ./cmd/telegram-mcp telegram-dry-run --source sellerproof_support --limit 5
```

## Telegram sync

Baseline a source without saving old history:

```bash
go run ./cmd/telegram-mcp telegram-sync --source mpwb_chat
```

On the first run without `--backfill`, sync stores only the latest message id in `source_states`.
It does not save old history.

Save the latest N messages manually:

```bash
go run ./cmd/telegram-mcp telegram-sync --source mpwb_chat --backfill 20
```

Save new messages after the stored cursor:

```bash
go run ./cmd/telegram-mcp telegram-sync --source mpwb_chat --limit 200
```

If there are more new messages than `--limit`, messages are saved but the cursor is not advanced.
Run again with a bigger limit to avoid silently skipping messages.
