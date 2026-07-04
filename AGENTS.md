# Agent instructions

## Project goal

This repository implements a read-only-first Telegram MCP server in Go.

The server exposes Telegram channels, groups, messages, comments, source state,
and spam/exclusion controls through MCP tools and resources. It is not an
autonomous agent and it is not based on Telegram Bot API polling.

## Architecture rules

- Keep `cmd` thin.
- Keep `internal/app` as the composition root.
- Domain types must not import infrastructure, MCP, Telegram, or storage packages.
- Telegram MTProto code must not contain spam policy decisions.
- MCP handlers must call application services instead of reading storage directly.
- Storage is the source of truth for sources, messages, state, and exclusions.
- Prefer explicit dependencies.
- Prefer small packages over generic helper packages.
- Do not add unnecessary dependencies, files, wrappers, or boilerplate.
- All exported Go types and functions must have doc comments.

## Safety rules

- Start read-only for Telegram itself.
- MCP write tools may change only local configuration/state, such as sources and spam list.
- Do not implement sending Telegram messages, joining chats, deleting messages, reactions, or moderation actions until explicitly approved.
- Store Telegram credentials and sessions locally. Never log secrets.
