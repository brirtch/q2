# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Q2 is a Go CLI application for managing folder paths with the following commands:
- `addfolder`: Add a folder to the database (validates folder exists)
- `removefolder`: Remove a folder from the database
- `listfolders`: List all stored folders
- `serve`: Run HTTP server with configurable port

## Build & Run Commands

**Build the binary:**
```bash
go build -o q2.exe .
```

**Run without building:**
```bash
go run . <command>
```

**Available commands:**
```bash
# Add a folder (must exist on filesystem)
go run . addfolder <folder_path>

# Remove a folder
go run . removefolder <folder_path>

# List all stored folders
go run . listfolders

# Start HTTP server (default port 8090)
go run . serve

# Start HTTP server on custom port
go run . serve -port 3000
```

**Run tests:**
```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run a specific test
go test -v -run TestAddFolder_Basic

# Run only db package tests
go test -v ./db/...
```

## Architecture

### CLI (main.go)

Command-line interface using Go's `flag` package with subcommands:

- `addFolder()`: Validates folder exists, normalizes path, stores in database
- `removeFolder()`: Removes folder from database by normalized path
- `listFolders()`: Queries and displays all stored folders
- `normalizePath()`: Platform-specific path handling (lowercase on Windows)
- `homeEndpoint()`: HTTP handler for root path

### Database (db/)

SQLite wrapper using the **Single Writer Pattern** to eliminate write lock contention:

- All writes are serialized through a single goroutine via a channel
- Reads use a connection pool for concurrent access
- WAL mode enabled for concurrent reads during writes

Key functions:
- `db.Open(path)`: Opens database, starts writer goroutine
- `db.Write(query, args...)`: Sends write to writer goroutine, blocks for result
- `db.Query/QueryRow`: Read operations using connection pool
- `db.Migrate()`: Applies pending migrations
- `db.Close()`: Graceful shutdown, drains pending writes

See `docs/design/sqlite-single-writer.txt` for design details.

### Migrations (migrations/)

Database migrations registered via `init()` functions:
- `001_create_folders`: Creates folders table
- `002_fix_case_sensitivity`: Removes COLLATE NOCASE, normalizes paths

### HTTP Server (serve command)

Endpoints:
- `GET /`: Simple health check, returns "Q2"
- `GET /browse`: File browser HTML page for navigating monitored folders
- `GET /schema`: Database schema viewer with formatted HTML display
- `GET /api/roots`: JSON list of monitored root folders
- `GET /api/browse?path=<path>`: JSON directory listing (path must be within a monitored folder)

### Data Storage

The `.q2/` directory is gitignored and contains:
- `q2.db`: SQLite database with folders table

### Path Handling

- Paths are normalized with `filepath.Clean()` and trailing quote removal
- On Windows: paths stored lowercase for case-insensitive matching
- On Linux: paths stored as-is for case-sensitive matching
- Duplicate detection uses normalized paths
