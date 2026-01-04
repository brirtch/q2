package migrations

import (
	"jukel.org/q2/db"
)

func init() {
	db.Register(db.Migration{
		ID: "004_create_scan_queue",
		Up: func(d *db.DB) error {
			result := d.Write(`
				CREATE TABLE scan_queue (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					path TEXT NOT NULL UNIQUE,
					requested_at DATETIME DEFAULT CURRENT_TIMESTAMP,
					started_at DATETIME,
					completed_at DATETIME
				)
			`)
			return result.Err
		},
		Down: func(d *db.DB) error {
			result := d.Write("DROP TABLE scan_queue")
			return result.Err
		},
	})
}
