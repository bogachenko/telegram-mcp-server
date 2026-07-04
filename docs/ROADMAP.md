# Roadmap

## Stage 1: repository scaffold

- Go module
- architecture docs
- domain models
- MCP tool names
- SQLite migration draft
- status command

## Stage 2: storage

- SQLite driver selection
- migration runner
- source repository
- message repository
- source state repository
- excluded sender repository

## Stage 3: MCP runtime

- choose MCP Go SDK
- stdio transport
- tool registration
- resource registration
- structured errors

## Stage 4: Telegram MTProto

- choose Go MTProto library
- local session storage
- auth flow
- source resolver
- scanner
- first-run baseline behavior

## Stage 5: exclusions

- `telegram.spam_add_sender`
- `telegram.spam_add_from_message`
- `telegram.spam_remove_sender`
- `telegram.spam_list_senders`
- hide existing messages by excluded sender
- skip excluded senders on future scan

## Stage 6: search and retrieval

- recent messages
- source messages
- message get by external id
- text search
- comment retrieval
