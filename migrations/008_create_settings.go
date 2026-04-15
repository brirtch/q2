package migrations

import (
	"jukel.org/q2/db"
)

func init() {
	db.Register(db.Migration{
		ID: "008_create_settings",
		Up: func(d *db.DB) error {
			result := d.Write(`
				CREATE TABLE settings (
					key TEXT PRIMARY KEY,
					value TEXT NOT NULL
				)
			`)
			return result.Err
		},
		Down: func(d *db.DB) error {
			result := d.Write("DROP TABLE settings")
			return result.Err
		},
	})
}
