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
go test

# Run tests with verbose output
go test -v

# Run a specific test
go test -v -run TestAddFolder_TrailingSlashDuplicate
```

## Architecture

This is a single-file application (main.go) with a simple command-line interface pattern:

- **Command parsing**: Uses Go's `flag` package with subcommands
- **Data storage**: `.q2/folders.txt` stores folder paths (one per line, case-insensitive deduplication)
- **HTTP server**: Basic server with single endpoint at `/` returning "Q2"

### Key Functions

- `main()` (main.go:78): Entry point, dispatches to subcommand handlers
- `addFolder()` (main.go:22): Handles folder path persistence with case-insensitive duplicate checking
- `homeEndpoint()` (main.go:15): HTTP handler for root path

### Data Storage

The `.q2/` directory is gitignored and contains:
- `folders.txt`: Newline-separated list of folder paths with case-insensitive uniqueness

When modifying folder management logic, ensure the case-insensitive duplicate checking (main.go:56) is preserved.
