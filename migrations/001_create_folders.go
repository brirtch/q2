package migrations

import (
	"jukel.org/q2/db"
)

func init() {
	db.Register(db.Migration{
		ID: "001_create_folders",
		Up: func(d *db.DB) error {
			result := d.Write(`
				CREATE TABLE folders (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					path TEXT NOT NULL UNIQUE COLLATE NOCASE,
					created_at DATETIME DEFAULT CURRENT_TIMESTAMP
				)
			`)
			return result.Err
		},
		Down: func(d *db.DB) error {
			result := d.Write("DROP TABLE folders")
			return result.Err
		},
	})
}
