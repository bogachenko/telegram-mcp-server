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
