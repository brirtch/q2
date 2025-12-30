// Package db provides a SQLite database wrapper using the Single Writer pattern
// to eliminate write lock contention. All writes are serialized through a single
// goroutine, while reads can happen concurrently via a connection pool.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

// WriteRequest represents a write operation to be executed by the writer goroutine.
type WriteRequest struct {
	Query  string
	Args   []any
	Result chan WriteResult
}

// WriteResult contains the result of a write operation.
type WriteResult struct {
	LastInsertID int64
	RowsAffected int64
	Err          error
}

// DB wraps SQLite with the Single Writer pattern.
// Reads use a connection pool, writes go through a dedicated channel.
type DB struct {
	readPool  *sql.DB
	writeConn *sql.DB
	writeChan chan WriteRequest
	done      chan struct{}
	wg        sync.WaitGroup
}

// Open creates a new DB instance with the Single Writer pattern.
// It opens separate connections for reading and writing, enables WAL mode,
// and starts the writer goroutine.
func Open(dbPath string) (*DB, error) {
	// Open read pool (multiple concurrent readers allowed)
	readPool, err := sql.Open("sqlite3", dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open read pool: %w", err)
	}

	// Configure read pool for concurrent access
	readPool.SetMaxOpenConns(10)
	readPool.SetMaxIdleConns(5)

	// Open write connection (single writer)
	writeConn, err := sql.Open("sqlite3", dbPath+"?mode=rwc&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		readPool.Close()
		return nil, fmt.Errorf("failed to open write connection: %w", err)
	}

	// Only one write connection
	writeConn.SetMaxOpenConns(1)
	writeConn.SetMaxIdleConns(1)

	// Enable WAL mode explicitly
	if _, err := writeConn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		readPool.Close()
		writeConn.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	db := &DB{
		readPool:  readPool,
		writeConn: writeConn,
		writeChan: make(chan WriteRequest, 100), // buffered for better throughput
		done:      make(chan struct{}),
	}

	// Start the writer goroutine
	db.wg.Add(1)
	go db.writerLoop()

	return db, nil
}

// writerLoop processes write requests sequentially.
// This is the core of the Single Writer pattern - all writes are serialized here.
func (db *DB) writerLoop() {
	defer db.wg.Done()

	for {
		select {
		case req := <-db.writeChan:
			result := db.executeWrite(req.Query, req.Args)
			req.Result <- result
		case <-db.done:
			// Drain remaining requests before shutting down
			for {
				select {
				case req := <-db.writeChan:
					result := db.executeWrite(req.Query, req.Args)
					req.Result <- result
				default:
					return
				}
			}
		}
	}
}

// executeWrite performs the actual write operation.
func (db *DB) executeWrite(query string, args []any) WriteResult {
	result, err := db.writeConn.Exec(query, args...)
	if err != nil {
		return WriteResult{Err: err}
	}

	lastID, _ := result.LastInsertId()
	rowsAffected, _ := result.RowsAffected()

	return WriteResult{
		LastInsertID: lastID,
		RowsAffected: rowsAffected,
	}
}

// Write sends a write request to the writer goroutine and waits for the result.
// This method is safe to call from multiple goroutines.
func (db *DB) Write(query string, args ...any) WriteResult {
	req := WriteRequest{
		Query:  query,
		Args:   args,
		Result: make(chan WriteResult, 1),
	}

	db.writeChan <- req
	return <-req.Result
}

// WriteContext sends a write request with context support for cancellation.
func (db *DB) WriteContext(ctx context.Context, query string, args ...any) WriteResult {
	req := WriteRequest{
		Query:  query,
		Args:   args,
		Result: make(chan WriteResult, 1),
	}

	select {
	case db.writeChan <- req:
		select {
		case result := <-req.Result:
			return result
		case <-ctx.Done():
			return WriteResult{Err: ctx.Err()}
		}
	case <-ctx.Done():
		return WriteResult{Err: ctx.Err()}
	}
}

// Query executes a read query and returns the rows.
// Safe for concurrent use - uses the read connection pool.
func (db *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return db.readPool.Query(query, args...)
}

// QueryContext executes a read query with context support.
func (db *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return db.readPool.QueryContext(ctx, query, args...)
}

// QueryRow executes a read query that returns at most one row.
func (db *DB) QueryRow(query string, args ...any) *sql.Row {
	return db.readPool.QueryRow(query, args...)
}

// QueryRowContext executes a read query with context support.
func (db *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return db.readPool.QueryRowContext(ctx, query, args...)
}

// Close gracefully shuts down the database connections.
// It signals the writer goroutine to stop, waits for pending writes to complete,
// and closes both connection pools.
func (db *DB) Close() error {
	close(db.done)
	db.wg.Wait()

	var errs []error
	if err := db.readPool.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close read pool: %w", err))
	}
	if err := db.writeConn.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close write connection: %w", err))
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// ReadPool returns the underlying read connection pool for advanced use cases.
// Use with caution - prefer the Query methods for normal operations.
func (db *DB) ReadPool() *sql.DB {
	return db.readPool
}
