package migrations

import (
	"jukel.org/q2/db"
)

func init() {
	db.Register(db.Migration{
		ID: "005_create_image_metadata",
		Up: func(d *db.DB) error {
			result := d.Write(`
				CREATE TABLE image_metadata (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					file_id INTEGER NOT NULL UNIQUE,
					camera_make TEXT,
					camera_model TEXT,
					date_taken DATETIME,
					width INTEGER,
					height INTEGER,
					orientation INTEGER,
					iso INTEGER,
					exposure_time TEXT,
					f_number REAL,
					focal_length REAL,
					gps_latitude REAL,
					gps_longitude REAL,
					FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
				)
			`)
			if result.Err != nil {
				return result.Err
			}

			result = d.Write(`CREATE INDEX idx_image_metadata_file_id ON image_metadata(file_id)`)
			return result.Err
		},
		Down: func(d *db.DB) error {
			result := d.Write("DROP TABLE image_metadata")
			return result.Err
		},
	})
}
