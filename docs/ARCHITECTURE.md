# Architecture

## Product boundary

`telegram-mcp-server` is a local MCP server. It does not make decisions as an
agent. It exposes controlled tools and resources so an MCP client can inspect
Telegram messages and manage local source/spam configuration.

## Layers

```text
cmd
  -> app

app
  -> mcp
  -> telegram
  -> storage
  -> config
  -> domain packages

mcp
  -> application services
  -> domain packages

telegram
  -> domain packages
  -> exclusion policy interface near scanner consumer

storage
  -> domain packages

domain
  -> stdlib only
```

## Packages

```text
cmd/telegram-mcp
  binary entry point

internal/app
  composition root and CLI runtime dispatch

internal/config
  environment and default paths

internal/domain
  source, message, sender, exclusion, and state models

internal/mcp
  MCP tool/resource definitions and server wiring

internal/telegram
  future MTProto client, source resolver, scanner, and normalizer

internal/storage
  future SQLite connection, repositories, and migrations
```

## Telegram source model

The Telegram layer must be MTProto/user-client based. Bot API `getUpdates` is out
of scope for the main product.

The scanner must:

1. Resolve configured sources.
2. Read messages after source state.
3. Normalize Telegram messages into domain messages.
4. Check exclusions through an interface.
5. Save allowed messages.
6. Advance source state even when a message is skipped by exclusion.

## Exclusion model

Spam/excluded senders are local policy. They are not Telegram moderation actions.

When a sender is excluded from a message, the server stores evidence:

- evidence message external id
- evidence message text
- evidence message link
- evidence message date
- evidence source id
- evidence source title

Existing messages are hidden, not deleted.
