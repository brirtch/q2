package migrations

import "jukel.org/q2/db"

func init() {
	db.Register(db.Migration{
		ID: "011_create_favourites",
		Up: func(d *db.DB) error {
			result := d.Write(`
				CREATE TABLE favourites (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					type TEXT NOT NULL,
					key TEXT NOT NULL,
					created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
					UNIQUE(type, key)
				)
			`)
			if result.Err != nil {
				return result.Err
			}
			result = d.Write(`CREATE INDEX idx_favourites_type ON favourites(type)`)
			return result.Err
		},
		Down: func(d *db.DB) error {
			return d.Write("DROP TABLE favourites").Err
		},
	})
}
