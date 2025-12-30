# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Q2 is a simple Go CLI application with two main commands:
- `addfolder`: Manages a list of folder paths in `.q2/folders.txt`
- `serve`: Runs a basic HTTP server on port 8090

## Build & Run Commands

**Build the binary:**
```bash
go build -o q2.exe .
```

**Run without building:**
```bash
go run main.go <command>
```

**Available commands:**
```bash
# Add a folder to the tracked list
go run main.go addfolder <folder_path>

# Start the HTTP server
go run main.go serve
```

**Run tests:**
```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run a specific test
go test -v -run TestAddFolder_TrailingSlashDuplicate

# Run only db package tests
go test -v ./db/...
```

## Architecture

### CLI (main.go)

Command-line interface using Go's `flag` package with subcommands:

- `main()` (main.go:78): Entry point, dispatches to subcommand handlers
- `addFolder()` (main.go:22): Handles folder path persistence with case-insensitive duplicate checking
- `homeEndpoint()` (main.go:15): HTTP handler for root path

### Database (db/)

SQLite wrapper using the **Single Writer Pattern** to eliminate write lock contention:

- All writes are serialized through a single goroutine via a channel
- Reads use a connection pool for concurrent access
- WAL mode enabled for concurrent reads during writes

Key types and functions:
- `db.Open(path)`: Opens database, starts writer goroutine, returns `*DB`
- `db.Write(query, args...)`: Sends write to writer goroutine, blocks for result
- `db.WriteContext(ctx, query, args...)`: Write with cancellation support
- `db.Query/QueryRow`: Read operations using connection pool
- `db.Close()`: Graceful shutdown, drains pending writes

See `docs/design/sqlite-single-writer.txt` for design details.

### Data Storage

The `.q2/` directory is gitignored and contains:
- `folders.txt`: Newline-separated list of folder paths with case-insensitive uniqueness
