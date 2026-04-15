package main

import (
	"fmt"
	"net/http"
	"strings"

	"jukel.org/q2/db"
)

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
        body { font-family: "Cascadia Code", "Fira Code", "JetBrains Mono", "SF Mono", Consolas, monospace; margin: 0; padding: 20px; background: #0d1117; color: #c9d1d9; }
        .container { max-width: 1000px; margin: 0 auto; }
        h1 { color: #58a6ff; margin-bottom: 30px; }
        h2 { color: #c9d1d9; margin-top: 30px; margin-bottom: 15px; border-bottom: 1px solid #30363d; padding-bottom: 5px; }
        .table-card { background: #161b22; border: 1px solid #30363d; border-radius: 6px; margin-bottom: 20px; overflow: hidden; }
        .table-header { background: #238636; color: white; padding: 15px 20px; font-size: 16px; font-weight: 600; }
        .table-header .icon { margin-right: 10px; }
        table { width: 100%; border-collapse: collapse; }
        th, td { padding: 12px 20px; text-align: left; border-bottom: 1px solid #21262d; }
        th { background: #0d1117; font-weight: 600; color: #8b949e; }
        tr:last-child td { border-bottom: none; }
        tr:hover { background: #1f2428; }
        .col-name { font-weight: 500; color: #c9d1d9; }
        .col-type { color: #7ee787; background: #23883622; padding: 2px 8px; border-radius: 4px; }
        .col-pk { background: #d29922; color: #0d1117; padding: 2px 8px; border-radius: 4px; font-size: 11px; font-weight: 600; margin-left: 8px; }
        .col-notnull { background: #f85149; color: white; padding: 2px 8px; border-radius: 4px; font-size: 11px; margin-left: 8px; }
        .col-default { color: #8b949e; font-size: 12px; }
        .sql-block { background: #0d1117; color: #7ee787; padding: 15px 20px; font-size: 12px; white-space: pre-wrap; word-break: break-all; border-top: 1px solid #30363d; }
        .index-card { background: #161b22; border: 1px solid #30363d; border-radius: 6px; margin-bottom: 10px; padding: 15px 20px; }
        .index-name { font-weight: 600; color: #c9d1d9; }
        .index-table { color: #8b949e; margin-left: 10px; }
        .index-unique { background: #238636; color: white; padding: 2px 8px; border-radius: 4px; font-size: 11px; margin-left: 8px; }
        .index-sql { font-size: 12px; color: #7ee787; margin-top: 8px; background: #0d1117; padding: 10px; border-radius: 4px; }
        .empty-message { color: #8b949e; font-style: italic; padding: 20px; }
        .home-link { display: inline-block; margin-bottom: 20px; color: #58a6ff; text-decoration: none; font-size: 14px; }
        .home-link:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <div class="container">
        <a href="/" class="home-link">← Home</a>
        <h1>&gt; schema_</h1>
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


// makeSettingsGetHandler creates a handler for GET /api/settings.
