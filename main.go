package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"jukel.org/q2/db"
	_ "jukel.org/q2/migrations"
	"jukel.org/q2/scanner"
)

const (
	q2Dir  = ".q2"
	dbFile = "q2.db"
)

// homeEndpoint responds with a simple "Q2" message.
func homeEndpoint(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(w, "Q2")
}

// API response types for file browser

// RootFolder represents a monitored folder.
type RootFolder struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// RootsResponse is the response for /api/roots.
type RootsResponse struct {
	Roots []RootFolder `json:"roots"`
}

// FileEntry represents a file or directory in a listing.
type FileEntry struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // "file" or "dir"
	Size     int64  `json:"size"`
	Modified string `json:"modified"` // ISO 8601 format
}

// BrowseResponse is the response for /api/browse.
type BrowseResponse struct {
	Path    string      `json:"path"`
	Parent  *string     `json:"parent"` // nil if this is a root folder
	Entries []FileEntry `json:"entries"`
}

// ErrorResponse is returned for API errors.
type ErrorResponse struct {
	Error string `json:"error"`
}

// getMonitoredFolders returns all monitored folder paths from the database.
func getMonitoredFolders(database *db.DB) ([]string, error) {
	rows, err := database.Query("SELECT path FROM folders ORDER BY path")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		folders = append(folders, path)
	}
	return folders, rows.Err()
}

// isPathWithinRoots checks if the given path is within one of the monitored folders.
// Returns the matching root folder path if valid, or empty string if not.
func isPathWithinRoots(path string, roots []string) string {
	normalizedPath := normalizePath(path)
	for _, root := range roots {
		normalizedRoot := normalizePath(root)
		// Check if path equals root or is a subdirectory of root
		if normalizedPath == normalizedRoot {
			return root
		}
		// Ensure we're checking for a proper subdirectory (with path separator)
		if strings.HasPrefix(normalizedPath, normalizedRoot+string(filepath.Separator)) {
			return root
		}
	}
	return ""
}

// listDirectory returns the contents of a directory.
func listDirectory(path string) ([]FileEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	result := make([]FileEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue // Skip entries we can't read
		}

		entryType := "file"
		if entry.IsDir() {
			entryType = "dir"
		}

		result = append(result, FileEntry{
			Name:     entry.Name(),
			Type:     entryType,
			Size:     info.Size(),
			Modified: info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	return result, nil
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// makeRootsHandler creates a handler for /api/roots.
func makeRootsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		folders, err := getMonitoredFolders(database)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
			return
		}

		roots := make([]RootFolder, len(folders))
		for i, path := range folders {
			roots[i] = RootFolder{
				Path: path,
				Name: filepath.Base(path),
			}
		}

		writeJSON(w, http.StatusOK, RootsResponse{Roots: roots})
	}
}

// makeBrowseHandler creates a handler for /api/browse.
func makeBrowseHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		path := r.URL.Query().Get("path")
		if path == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path parameter required"})
			return
		}

		// Clean the path
		path, ok := cleanPath(path)
		if !ok {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid path"})
			return
		}

		// Get monitored folders
		roots, err := getMonitoredFolders(database)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
			return
		}

		// Verify path is within a monitored folder
		matchedRoot := isPathWithinRoots(path, roots)
		if matchedRoot == "" {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "path not within monitored folders"})
			return
		}

		// Check if path exists and is a directory
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "path not found"})
			} else {
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "cannot access path"})
			}
			return
		}
		if !info.IsDir() {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path is not a directory"})
			return
		}

		// List directory contents
		entries, err := listDirectory(path)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "cannot read directory"})
			return
		}

		// Determine parent path (nil if this is a root folder)
		var parent *string
		normalizedPath := normalizePath(path)
		normalizedRoot := normalizePath(matchedRoot)
		if normalizedPath != normalizedRoot {
			parentPath := filepath.Dir(path)
			parent = &parentPath
		}

		writeJSON(w, http.StatusOK, BrowseResponse{
			Path:    path,
			Parent:  parent,
			Entries: entries,
		})
	}
}

// browsePageHTML is the embedded HTML template for the file browser.
const browsePageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Q2 File Browser</title>
    <style>
        * { box-sizing: border-box; }
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; margin: 0; padding: 20px; background: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; background: white; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        h1 { margin: 0; padding: 20px; border-bottom: 1px solid #eee; font-size: 24px; }
        .breadcrumb { padding: 15px 20px; background: #fafafa; border-bottom: 1px solid #eee; }
        .breadcrumb a { color: #0066cc; text-decoration: none; }
        .breadcrumb a:hover { text-decoration: underline; }
        .breadcrumb span { color: #666; margin: 0 8px; }
        table { width: 100%; border-collapse: collapse; }
        th, td { padding: 12px 20px; text-align: left; border-bottom: 1px solid #eee; }
        th { background: #fafafa; cursor: pointer; user-select: none; font-weight: 600; }
        th:hover { background: #f0f0f0; }
        th .sort-indicator { margin-left: 5px; color: #999; }
        tr:hover { background: #f8f8f8; }
        .name-cell { display: flex; align-items: center; gap: 10px; }
        .icon { font-size: 18px; }
        .folder-link { color: #0066cc; text-decoration: none; cursor: pointer; }
        .folder-link:hover { text-decoration: underline; }
        .file-name { color: #333; }
        .size-cell, .modified-cell { color: #666; }
        .type-cell { color: #888; text-transform: uppercase; font-size: 12px; }
        .empty-message { padding: 40px; text-align: center; color: #666; }
        .error-message { padding: 40px; text-align: center; color: #cc0000; }
        .loading { padding: 40px; text-align: center; color: #666; }
        .roots-list { padding: 20px; }
        .root-item { display: flex; align-items: center; gap: 10px; padding: 15px; border: 1px solid #eee; border-radius: 6px; margin-bottom: 10px; cursor: pointer; }
        .root-item:hover { background: #f8f8f8; border-color: #ddd; }
        .root-item .icon { font-size: 24px; }
        .root-item .path { color: #666; font-size: 14px; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Q2 File Browser</h1>
        <div class="breadcrumb" id="breadcrumb"></div>
        <div id="content"><div class="loading">Loading...</div></div>
    </div>

    <script>
        let currentPath = null;
        let currentEntries = [];
        let sortColumn = 'name';
        let sortAsc = true;

        function formatSize(bytes) {
            if (bytes === 0) return '-';
            const units = ['B', 'KB', 'MB', 'GB', 'TB'];
            const i = Math.floor(Math.log(bytes) / Math.log(1024));
            return (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + units[i];
        }

        function formatDate(isoString) {
            const date = new Date(isoString);
            return date.toLocaleDateString() + ' ' + date.toLocaleTimeString();
        }

        function getExtension(name) {
            const idx = name.lastIndexOf('.');
            return idx > 0 ? name.substring(idx + 1).toLowerCase() : '';
        }

        function sortEntries(entries, column, asc) {
            return [...entries].sort((a, b) => {
                // Directories always first
                if (a.type !== b.type) {
                    return a.type === 'dir' ? -1 : 1;
                }
                let cmp = 0;
                switch (column) {
                    case 'name':
                        cmp = a.name.localeCompare(b.name, undefined, {sensitivity: 'base'});
                        break;
                    case 'type':
                        cmp = getExtension(a.name).localeCompare(getExtension(b.name));
                        break;
                    case 'size':
                        cmp = a.size - b.size;
                        break;
                    case 'modified':
                        cmp = new Date(a.modified) - new Date(b.modified);
                        break;
                }
                return asc ? cmp : -cmp;
            });
        }

        function renderBreadcrumb(path, parent) {
            const bc = document.getElementById('breadcrumb');
            let html = '<a href="#" onclick="loadRoots(); return false;">Roots</a>';
            if (path) {
                const parts = path.split(/[\\/]/);
                let accumulated = '';
                for (let i = 0; i < parts.length; i++) {
                    if (parts[i] === '') continue;
                    accumulated += (i === 0 || accumulated === '' ? '' : (path.includes('\\') ? '\\' : '/')) + parts[i];
                    if (i === 0 && path.match(/^[a-zA-Z]:/)) {
                        accumulated = parts[i];
                    }
                    html += ' <span>/</span> ';
                    if (i === parts.length - 1) {
                        html += '<strong>' + parts[i] + '</strong>';
                    } else {
                        html += '<a href="#" onclick="browse(\'' + accumulated.replace(/\\/g, '\\\\') + '\'); return false;">' + parts[i] + '</a>';
                    }
                }
            }
            bc.innerHTML = html;
        }

        function renderTable() {
            const sorted = sortEntries(currentEntries, sortColumn, sortAsc);
            const indicator = (col) => sortColumn === col ? (sortAsc ? ' ▲' : ' ▼') : '';

            let html = '<table><thead><tr>';
            html += '<th onclick="changeSort(\'name\')">Name<span class="sort-indicator">' + indicator('name') + '</span></th>';
            html += '<th onclick="changeSort(\'type\')">Type<span class="sort-indicator">' + indicator('type') + '</span></th>';
            html += '<th onclick="changeSort(\'size\')">Size<span class="sort-indicator">' + indicator('size') + '</span></th>';
            html += '<th onclick="changeSort(\'modified\')">Modified<span class="sort-indicator">' + indicator('modified') + '</span></th>';
            html += '</tr></thead><tbody>';

            if (sorted.length === 0) {
                html += '<tr><td colspan="4" class="empty-message">This folder is empty</td></tr>';
            } else {
                for (const entry of sorted) {
                    const icon = entry.type === 'dir' ? '📁' : '📄';
                    const fullPath = currentPath + (currentPath.includes('\\') ? '\\' : '/') + entry.name;
                    html += '<tr>';
                    html += '<td class="name-cell"><span class="icon">' + icon + '</span>';
                    if (entry.type === 'dir') {
                        html += '<a class="folder-link" onclick="browse(\'' + fullPath.replace(/\\/g, '\\\\').replace(/'/g, "\\'") + '\')">' + entry.name + '</a>';
                    } else {
                        html += '<span class="file-name">' + entry.name + '</span>';
                    }
                    html += '</td>';
                    html += '<td class="type-cell">' + (entry.type === 'dir' ? 'Folder' : getExtension(entry.name) || 'File') + '</td>';
                    html += '<td class="size-cell">' + (entry.type === 'dir' ? '-' : formatSize(entry.size)) + '</td>';
                    html += '<td class="modified-cell">' + formatDate(entry.modified) + '</td>';
                    html += '</tr>';
                }
            }
            html += '</tbody></table>';
            document.getElementById('content').innerHTML = html;
        }

        function changeSort(column) {
            if (sortColumn === column) {
                sortAsc = !sortAsc;
            } else {
                sortColumn = column;
                sortAsc = true;
            }
            renderTable();
        }

        async function loadRoots() {
            currentPath = null;
            renderBreadcrumb(null, null);
            document.getElementById('content').innerHTML = '<div class="loading">Loading...</div>';

            try {
                const resp = await fetch('/api/roots');
                const data = await resp.json();

                if (data.error) {
                    document.getElementById('content').innerHTML = '<div class="error-message">' + data.error + '</div>';
                    return;
                }

                if (data.roots.length === 0) {
                    document.getElementById('content').innerHTML = '<div class="empty-message">No monitored folders. Use "q2 addfolder &lt;path&gt;" to add folders.</div>';
                    return;
                }

                let html = '<div class="roots-list">';
                for (const root of data.roots) {
                    html += '<div class="root-item" onclick="browse(\'' + root.path.replace(/\\/g, '\\\\').replace(/'/g, "\\'") + '\')">';
                    html += '<span class="icon">📁</span>';
                    html += '<div><strong>' + root.name + '</strong><div class="path">' + root.path + '</div></div>';
                    html += '</div>';
                }
                html += '</div>';
                document.getElementById('content').innerHTML = html;
            } catch (e) {
                document.getElementById('content').innerHTML = '<div class="error-message">Failed to load: ' + e.message + '</div>';
            }
        }

        async function browse(path) {
            document.getElementById('content').innerHTML = '<div class="loading">Loading...</div>';

            try {
                const resp = await fetch('/api/browse?path=' + encodeURIComponent(path));
                const data = await resp.json();

                if (data.error) {
                    document.getElementById('content').innerHTML = '<div class="error-message">' + data.error + '</div>';
                    return;
                }

                currentPath = data.path;
                currentEntries = data.entries;
                sortColumn = 'name';
                sortAsc = true;
                renderBreadcrumb(data.path, data.parent);
                renderTable();
            } catch (e) {
                document.getElementById('content').innerHTML = '<div class="error-message">Failed to load: ' + e.message + '</div>';
            }
        }

        // Initial load
        loadRoots();
    </script>
</body>
</html>`

// browsePageHandler serves the file browser HTML page.
func browsePageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(browsePageHTML))
}

// ColumnInfo holds information about a table column.
type ColumnInfo struct {
	CID     int
	Name    string
	Type    string
	NotNull bool
	Default *string
	PK      int
}

// TableInfo holds schema information for a table.
type TableInfo struct {
	Name    string
	SQL     string
	Columns []ColumnInfo
}

// IndexInfo holds information about an index.
type IndexInfo struct {
	Name   string
	Table  string
	SQL    string
	Unique bool
}

// makeSchemaHandler creates a handler for /schema.
func makeSchemaHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get all tables
		rows, err := database.Query(`
			SELECT name, sql FROM sqlite_master
			WHERE type='table' AND name NOT LIKE 'sqlite_%'
			ORDER BY name
		`)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var tables []TableInfo
		for rows.Next() {
			var t TableInfo
			var sql *string
			if err := rows.Scan(&t.Name, &sql); err != nil {
				continue
			}
			if sql != nil {
				t.SQL = *sql
			}
			tables = append(tables, t)
		}

		// Get column info for each table
		for i := range tables {
			colRows, err := database.Query(fmt.Sprintf("PRAGMA table_info(%s)", tables[i].Name))
			if err != nil {
				continue
			}
			for colRows.Next() {
				var col ColumnInfo
				var notNull int
				if err := colRows.Scan(&col.CID, &col.Name, &col.Type, &notNull, &col.Default, &col.PK); err != nil {
					continue
				}
				col.NotNull = notNull == 1
				tables[i].Columns = append(tables[i].Columns, col)
			}
			colRows.Close()
		}

		// Get all indexes
		idxRows, err := database.Query(`
			SELECT name, tbl_name, sql FROM sqlite_master
			WHERE type='index' AND name NOT LIKE 'sqlite_%'
			ORDER BY tbl_name, name
		`)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer idxRows.Close()

		var indexes []IndexInfo
		for idxRows.Next() {
			var idx IndexInfo
			var sql *string
			if err := idxRows.Scan(&idx.Name, &idx.Table, &sql); err != nil {
				continue
			}
			if sql != nil {
				idx.SQL = *sql
				idx.Unique = strings.Contains(strings.ToUpper(*sql), "UNIQUE")
			}
			indexes = append(indexes, idx)
		}

		// Render HTML
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Q2 Database Schema</title>
    <style>
        * { box-sizing: border-box; }
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; margin: 0; padding: 20px; background: #f5f5f5; }
        .container { max-width: 1000px; margin: 0 auto; }
        h1 { color: #333; margin-bottom: 30px; }
        h2 { color: #444; margin-top: 30px; margin-bottom: 15px; border-bottom: 2px solid #0066cc; padding-bottom: 5px; }
        .table-card { background: white; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); margin-bottom: 20px; overflow: hidden; }
        .table-header { background: #0066cc; color: white; padding: 15px 20px; font-size: 18px; font-weight: 600; }
        .table-header .icon { margin-right: 10px; }
        table { width: 100%; border-collapse: collapse; }
        th, td { padding: 12px 20px; text-align: left; border-bottom: 1px solid #eee; }
        th { background: #fafafa; font-weight: 600; color: #555; }
        tr:last-child td { border-bottom: none; }
        tr:hover { background: #f8f8f8; }
        .col-name { font-weight: 500; color: #333; }
        .col-type { font-family: monospace; color: #0066cc; background: #f0f7ff; padding: 2px 8px; border-radius: 4px; }
        .col-pk { background: #ffc107; color: #333; padding: 2px 8px; border-radius: 4px; font-size: 12px; font-weight: 600; margin-left: 8px; }
        .col-notnull { background: #dc3545; color: white; padding: 2px 8px; border-radius: 4px; font-size: 12px; margin-left: 8px; }
        .col-default { color: #666; font-family: monospace; font-size: 13px; }
        .sql-block { background: #2d2d2d; color: #f8f8f2; padding: 15px 20px; font-family: monospace; font-size: 13px; white-space: pre-wrap; word-break: break-all; border-top: 1px solid #444; }
        .index-card { background: white; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); margin-bottom: 10px; padding: 15px 20px; }
        .index-name { font-weight: 600; color: #333; }
        .index-table { color: #666; margin-left: 10px; }
        .index-unique { background: #28a745; color: white; padding: 2px 8px; border-radius: 4px; font-size: 12px; margin-left: 8px; }
        .index-sql { font-family: monospace; font-size: 13px; color: #555; margin-top: 8px; background: #f5f5f5; padding: 10px; border-radius: 4px; }
        .empty-message { color: #666; font-style: italic; padding: 20px; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Q2 Database Schema</h1>
        <h2>Tables</h2>
`)

		if len(tables) == 0 {
			fmt.Fprint(w, `<p class="empty-message">No tables found.</p>`)
		}

		for _, t := range tables {
			fmt.Fprintf(w, `<div class="table-card">
            <div class="table-header"><span class="icon">📊</span>%s</div>
            <table>
                <thead>
                    <tr>
                        <th>Column</th>
                        <th>Type</th>
                        <th>Constraints</th>
                        <th>Default</th>
                    </tr>
                </thead>
                <tbody>
`, t.Name)

			for _, col := range t.Columns {
				constraints := ""
				if col.PK > 0 {
					constraints += `<span class="col-pk">PK</span>`
				}
				if col.NotNull {
					constraints += `<span class="col-notnull">NOT NULL</span>`
				}
				defaultVal := "-"
				if col.Default != nil {
					defaultVal = fmt.Sprintf(`<span class="col-default">%s</span>`, *col.Default)
				}
				fmt.Fprintf(w, `                    <tr>
                        <td class="col-name">%s</td>
                        <td><span class="col-type">%s</span></td>
                        <td>%s</td>
                        <td>%s</td>
                    </tr>
`, col.Name, col.Type, constraints, defaultVal)
			}

			fmt.Fprintf(w, `                </tbody>
            </table>
            <div class="sql-block">%s</div>
        </div>
`, t.SQL)
		}

		fmt.Fprint(w, `<h2>Indexes</h2>`)

		if len(indexes) == 0 {
			fmt.Fprint(w, `<p class="empty-message">No indexes found.</p>`)
		}

		for _, idx := range indexes {
			unique := ""
			if idx.Unique {
				unique = `<span class="index-unique">UNIQUE</span>`
			}
			fmt.Fprintf(w, `<div class="index-card">
            <div><span class="index-name">%s</span><span class="index-table">on %s</span>%s</div>
            <div class="index-sql">%s</div>
        </div>
`, idx.Name, idx.Table, unique, idx.SQL)
		}

		fmt.Fprint(w, `    </div>
</body>
</html>`)
	}
}

// initDB initializes the database and runs migrations.
func initDB(baseDir string) (*db.DB, error) {
	// Ensure .q2 directory exists
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", baseDir, err)
	}

	dbPath := filepath.Join(baseDir, dbFile)
	database, err := db.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := database.Migrate(); err != nil {
		database.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return database, nil
}

// cleanPath trims whitespace and removes stray quote characters from shell escaping issues.
// Returns the cleaned path and true if non-empty, or empty string and false if empty.
func cleanPath(path string) (string, bool) {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, `"'`)
	if path == "" {
		return "", false
	}
	return filepath.Clean(path), true
}

// normalizePath cleans the path and applies platform-specific normalization.
// On Windows, paths are lowercased for case-insensitive comparison.
// On Linux/macOS, paths are kept as-is for case-sensitive comparison.
func normalizePath(path string) string {
	path, _ = cleanPath(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

// addFolder adds the given folder path to the database.
// It ensures the folder exists and no duplicate entries are added.
// Case sensitivity matches the platform (case-insensitive on Windows, case-sensitive on Linux).
// Returns an error if the folder is empty, doesn't exist, or a database error occurs.
func addFolder(folder string, database *db.DB) error {
	folder, ok := cleanPath(folder)
	if !ok {
		return errors.New("folder cannot be empty")
	}

	// Check if folder exists
	info, err := os.Stat(folder)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("folder does not exist: %s", folder)
		}
		return fmt.Errorf("cannot access folder: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", folder)
	}

	// Normalize path for storage (lowercase on Windows)
	normalizedPath := normalizePath(folder)

	// Try to insert - will fail if duplicate due to UNIQUE constraint
	result := database.Write(
		"INSERT OR IGNORE INTO folders (path) VALUES (?)",
		normalizedPath,
	)
	if result.Err != nil {
		return result.Err
	}

	if result.RowsAffected == 0 {
		fmt.Printf("Folder %s already exists\n", folder)
	} else {
		fmt.Printf("Folder %s added\n", folder)
	}

	return nil
}

// removeFolder removes a folder from the database.
// Returns an error if the folder is empty or not found.
func removeFolder(folder string, database *db.DB) error {
	folder, ok := cleanPath(folder)
	if !ok {
		return errors.New("folder cannot be empty")
	}

	normalizedPath := normalizePath(folder)

	result := database.Write("DELETE FROM folders WHERE path = ?", normalizedPath)
	if result.Err != nil {
		return result.Err
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("folder not found: %s", folder)
	}

	fmt.Printf("Folder %s removed\n", folder)
	return nil
}

// listFolders retrieves and displays all stored folders from the database.
func listFolders(database *db.DB) error {
	rows, err := database.Query("SELECT path FROM folders ORDER BY path")
	if err != nil {
		return fmt.Errorf("failed to query folders: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return fmt.Errorf("failed to read folder: %w", err)
		}
		fmt.Println(path)
		count++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error reading folders: %w", err)
	}

	if count == 0 {
		fmt.Println("No folders stored")
	}

	return nil
}

// main parses subcommands and dispatches to the appropriate handler.
// Supported commands: addfolder, removefolder, listfolders, serve
func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s <command> [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  addfolder	Add a folder to Q2\n")
		fmt.Fprintf(os.Stderr, "  removefolder	Remove a folder from Q2\n")
		fmt.Fprintf(os.Stderr, "  listfolders	List stored folders\n")
		fmt.Fprintf(os.Stderr, "  scan		Scan a folder for files\n")
		fmt.Fprintf(os.Stderr, "  serve		Start serving Q2\n")
	}

	if len(os.Args) < 2 {
		flag.Usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "addfolder":
		addFolderCmd := flag.NewFlagSet("addfolder", flag.ContinueOnError)

		addFolderCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "Usage: \n")
			fmt.Fprintf(os.Stderr, "  %s addfolder <folder>\n\n", os.Args[0])
			addFolderCmd.PrintDefaults()
		}
		if err := addFolderCmd.Parse(os.Args[2:]); err != nil {
			addFolderCmd.Usage()
			os.Exit(2)
		}

		args := addFolderCmd.Args()

		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "addfolder requires exactly one <folder>")
			addFolderCmd.Usage()
			os.Exit(2)
		}

		folder := args[0]

		database, err := initDB(q2Dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error initializing database:", err)
			os.Exit(1)
		}
		defer database.Close()

		if err := addFolder(folder, database); err != nil {
			fmt.Fprintln(os.Stderr, "Error adding folder:", err)
			os.Exit(1)
		}

	case "removefolder":
		removeFolderCmd := flag.NewFlagSet("removefolder", flag.ContinueOnError)

		removeFolderCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "Usage: \n")
			fmt.Fprintf(os.Stderr, "  %s removefolder <folder>\n\n", os.Args[0])
			removeFolderCmd.PrintDefaults()
		}
		if err := removeFolderCmd.Parse(os.Args[2:]); err != nil {
			removeFolderCmd.Usage()
			os.Exit(2)
		}

		args := removeFolderCmd.Args()

		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "removefolder requires exactly one <folder>")
			removeFolderCmd.Usage()
			os.Exit(2)
		}

		folder := args[0]

		database, err := initDB(q2Dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error initializing database:", err)
			os.Exit(1)
		}
		defer database.Close()

		if err := removeFolder(folder, database); err != nil {
			fmt.Fprintln(os.Stderr, "Error removing folder:", err)
			os.Exit(1)
		}

	case "listfolders":
		database, err := initDB(q2Dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error initializing database:", err)
			os.Exit(1)
		}
		defer database.Close()

		if err := listFolders(database); err != nil {
			fmt.Fprintln(os.Stderr, "Error listing folders:", err)
			os.Exit(1)
		}

	case "scan":
		scanCmd := flag.NewFlagSet("scan", flag.ContinueOnError)

		scanCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "Usage: \n")
			fmt.Fprintf(os.Stderr, "  %s scan <folder>\n\n", os.Args[0])
			fmt.Fprintf(os.Stderr, "Scans a folder for files and adds them to the database.\n")
			fmt.Fprintf(os.Stderr, "The folder must be within a monitored folder.\n\n")
			scanCmd.PrintDefaults()
		}

		if err := scanCmd.Parse(os.Args[2:]); err != nil {
			scanCmd.Usage()
			os.Exit(2)
		}

		args := scanCmd.Args()

		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "scan requires exactly one <folder>")
			scanCmd.Usage()
			os.Exit(2)
		}

		folder := args[0]

		// Clean and validate the folder path
		folder, ok := cleanPath(folder)
		if !ok {
			fmt.Fprintln(os.Stderr, "Error: folder cannot be empty")
			os.Exit(1)
		}

		// Check if folder exists
		info, err := os.Stat(folder)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "Error: folder does not exist: %s\n", folder)
			} else {
				fmt.Fprintf(os.Stderr, "Error: cannot access folder: %v\n", err)
			}
			os.Exit(1)
		}
		if !info.IsDir() {
			fmt.Fprintf(os.Stderr, "Error: path is not a directory: %s\n", folder)
			os.Exit(1)
		}

		database, err := initDB(q2Dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error initializing database:", err)
			os.Exit(1)
		}
		defer database.Close()

		// Find the parent monitored folder
		parentPath, folderID, err := scanner.FindParentFolder(database, folder)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Scanning %s (monitored folder: %s)...\n", folder, parentPath)

		// Perform the scan
		result, err := scanner.ScanFolder(database, folder, folderID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error scanning folder: %v\n", err)
			os.Exit(1)
		}

		// Report results
		fmt.Printf("Scan complete: %d added, %d updated, %d removed\n",
			result.FilesAdded, result.FilesUpdated, result.FilesRemoved)

		if len(result.Errors) > 0 {
			fmt.Printf("%d errors encountered:\n", len(result.Errors))
			for _, e := range result.Errors {
				fmt.Printf("  - %v\n", e)
			}
		}

	case "serve":
		serveCmd := flag.NewFlagSet("serve", flag.ContinueOnError)
		port := serveCmd.Int("port", 8090, "Port to listen on")

		serveCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "Usage: \n")
			fmt.Fprintf(os.Stderr, "  %s serve [options]\n\n", os.Args[0])
			fmt.Fprintf(os.Stderr, "Options:\n")
			serveCmd.PrintDefaults()
		}

		if err := serveCmd.Parse(os.Args[2:]); err != nil {
			serveCmd.Usage()
			os.Exit(2)
		}

		database, err := initDB(q2Dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error initializing database:", err)
			os.Exit(1)
		}
		defer database.Close()

		fmt.Println("Q2")

		// Set up HTTP handlers
		mux := http.NewServeMux()
		mux.HandleFunc("/", homeEndpoint)
		mux.HandleFunc("/browse", browsePageHandler)
		mux.HandleFunc("/schema", makeSchemaHandler(database))
		mux.HandleFunc("/api/roots", makeRootsHandler(database))
		mux.HandleFunc("/api/browse", makeBrowseHandler(database))

		addr := fmt.Sprintf(":%d", *port)
		server := &http.Server{
			Addr:    addr,
			Handler: mux,
		}

		// Handle shutdown signals
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		// Start server in goroutine
		go func() {
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				fmt.Fprintln(os.Stderr, "Server error:", err)
				os.Exit(1)
			}
		}()

		fmt.Printf("Listening on port %s\n", addr)

		// Wait for shutdown signal
		<-sigChan
		fmt.Println("\nShutting down...")

		// Shutdown HTTP server with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "Server shutdown error:", err)
		}

		fmt.Println("Shutdown complete")

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		flag.Usage()
		os.Exit(2)

	}

}
