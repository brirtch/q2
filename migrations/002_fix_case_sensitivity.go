package migrations

import (
	"runtime"
	"strings"

	"jukel.org/q2/db"
)

func init() {
	db.Register(db.Migration{
		ID: "002_fix_case_sensitivity",
		Up: func(d *db.DB) error {
			// Create new table without COLLATE NOCASE
			// Case sensitivity is now handled at the application level
			result := d.Write(`
				CREATE TABLE folders_new (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					path TEXT NOT NULL UNIQUE,
					created_at DATETIME DEFAULT CURRENT_TIMESTAMP
				)
			`)
			if result.Err != nil {
				return result.Err
			}

			// Migrate existing data, normalizing paths on Windows
			rows, err := d.Query("SELECT path, created_at FROM folders")
			if err != nil {
				return err
			}
			defer rows.Close()

			for rows.Next() {
				var path string
				var createdAt string
				if err := rows.Scan(&path, &createdAt); err != nil {
					return err
				}

				// Normalize to lowercase on Windows
				if runtime.GOOS == "windows" {
					path = strings.ToLower(path)
				}

				result := d.Write(
					"INSERT OR IGNORE INTO folders_new (path, created_at) VALUES (?, ?)",
					path, createdAt,
				)
				if result.Err != nil {
					return result.Err
				}
			}
			if err := rows.Err(); err != nil {
				return err
			}

			// Drop old table and rename new one
			result = d.Write("DROP TABLE folders")
			if result.Err != nil {
				return result.Err
			}

			result = d.Write("ALTER TABLE folders_new RENAME TO folders")
			return result.Err
		},
		Down: func(d *db.DB) error {
			// Recreate original table with COLLATE NOCASE
			result := d.Write(`
				CREATE TABLE folders_old (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					path TEXT NOT NULL UNIQUE COLLATE NOCASE,
					created_at DATETIME DEFAULT CURRENT_TIMESTAMP
				)
			`)
			if result.Err != nil {
				return result.Err
			}

			// Migrate data back
			rows, err := d.Query("SELECT path, created_at FROM folders")
			if err != nil {
				return err
			}
			defer rows.Close()

			for rows.Next() {
				var path, createdAt string
				if err := rows.Scan(&path, &createdAt); err != nil {
					return err
				}
				result := d.Write(
					"INSERT OR IGNORE INTO folders_old (path, created_at) VALUES (?, ?)",
					path, createdAt,
				)
				if result.Err != nil {
					return result.Err
				}
			}
			if err := rows.Err(); err != nil {
				return err
			}

			result = d.Write("DROP TABLE folders")
			if result.Err != nil {
				return result.Err
			}

			result = d.Write("ALTER TABLE folders_old RENAME TO folders")
			return result.Err
		},
	})
}
