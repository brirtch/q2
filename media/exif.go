package media

import (
	"os"
	"time"

	"github.com/rwcarlsen/goexif/exif"
	"jukel.org/q2/db"
)

// ImageMetadata contains extracted EXIF data from an image.
type ImageMetadata struct {
	CameraMake   *string
	CameraModel  *string
	DateTaken    *time.Time
	Width        *int
	Height       *int
	Orientation  *int
	ISO          *int
	ExposureTime *string
	FNumber      *float64
	FocalLength  *float64
	GPSLatitude  *float64
	GPSLongitude *float64
}

// ExtractEXIF extracts EXIF metadata from an image file.
func ExtractEXIF(imagePath string) (*ImageMetadata, error) {
	file, err := os.Open(imagePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	x, err := exif.Decode(file)
	if err != nil {
		// No EXIF data or unsupported format - return empty metadata
		return &ImageMetadata{}, nil
	}

	meta := &ImageMetadata{}

	// Camera make
	if tag, err := x.Get(exif.Make); err == nil {
		if val, err := tag.StringVal(); err == nil {
			meta.CameraMake = &val
		}
	}

	// Camera model
	if tag, err := x.Get(exif.Model); err == nil {
		if val, err := tag.StringVal(); err == nil {
			meta.CameraModel = &val
		}
	}

	// Date taken
	if tm, err := x.DateTime(); err == nil {
		meta.DateTaken = &tm
	}

	// Image dimensions
	if tag, err := x.Get(exif.PixelXDimension); err == nil {
		if val, err := tag.Int(0); err == nil {
			meta.Width = &val
		}
	}
	if tag, err := x.Get(exif.PixelYDimension); err == nil {
		if val, err := tag.Int(0); err == nil {
			meta.Height = &val
		}
	}

	// Orientation
	if tag, err := x.Get(exif.Orientation); err == nil {
		if val, err := tag.Int(0); err == nil {
			meta.Orientation = &val
		}
	}

	// ISO
	if tag, err := x.Get(exif.ISOSpeedRatings); err == nil {
		if val, err := tag.Int(0); err == nil {
			meta.ISO = &val
		}
	}

	// Exposure time
	if tag, err := x.Get(exif.ExposureTime); err == nil {
		if num, denom, err := tag.Rat2(0); err == nil {
			if denom != 0 {
				exp := formatExposureTime(num, denom)
				meta.ExposureTime = &exp
			}
		}
	}

	// F-number (aperture)
	if tag, err := x.Get(exif.FNumber); err == nil {
		if num, denom, err := tag.Rat2(0); err == nil {
			if denom != 0 {
				fnum := float64(num) / float64(denom)
				meta.FNumber = &fnum
			}
		}
	}

	// Focal length
	if tag, err := x.Get(exif.FocalLength); err == nil {
		if num, denom, err := tag.Rat2(0); err == nil {
			if denom != 0 {
				fl := float64(num) / float64(denom)
				meta.FocalLength = &fl
			}
		}
	}

	// GPS coordinates
	if lat, lon, err := x.LatLong(); err == nil {
		meta.GPSLatitude = &lat
		meta.GPSLongitude = &lon
	}

	return meta, nil
}

// formatExposureTime formats exposure time as a human-readable string.
func formatExposureTime(num, denom int64) string {
	if num >= denom {
		// Exposure time >= 1 second
		return formatFloat(float64(num) / float64(denom))
	}
	// Exposure time < 1 second, show as fraction
	// Simplify the fraction
	gcd := gcdInt64(num, denom)
	num /= gcd
	denom /= gcd
	return formatFraction(num, denom)
}

func formatFloat(f float64) string {
	if f == float64(int64(f)) {
		return formatInt64(int64(f))
	}
	return formatFloatPrecision(f, 2)
}

func formatInt64(n int64) string {
	s := ""
	if n < 0 {
		s = "-"
		n = -n
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if len(digits) == 0 {
		return "0"
	}
	return s + string(digits)
}

func formatFloatPrecision(f float64, prec int) string {
	// Simple float formatting
	intPart := int64(f)
	fracPart := f - float64(intPart)
	if fracPart < 0 {
		fracPart = -fracPart
	}

	result := formatInt64(intPart) + "."
	for i := 0; i < prec; i++ {
		fracPart *= 10
		digit := int(fracPart)
		result += string(byte('0' + digit))
		fracPart -= float64(digit)
	}
	return result
}

func formatFraction(num, denom int64) string {
	return formatInt64(num) + "/" + formatInt64(denom)
}

func gcdInt64(a, b int64) int64 {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// SaveImageMetadata saves image metadata to the database.
func SaveImageMetadata(database *db.DB, fileID int64, meta *ImageMetadata) error {
	// Check if metadata already exists
	var existingID int64
	row := database.QueryRow("SELECT id FROM image_metadata WHERE file_id = ?", fileID)
	if err := row.Scan(&existingID); err == nil {
		// Already exists, skip
		return nil
	}

	result := database.Write(`
		INSERT INTO image_metadata (
			file_id, camera_make, camera_model, date_taken,
			width, height, orientation, iso,
			exposure_time, f_number, focal_length,
			gps_latitude, gps_longitude
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		fileID, meta.CameraMake, meta.CameraModel, meta.DateTaken,
		meta.Width, meta.Height, meta.Orientation, meta.ISO,
		meta.ExposureTime, meta.FNumber, meta.FocalLength,
		meta.GPSLatitude, meta.GPSLongitude,
	)

	return result.Err
}

// HasImageMetadata checks if a file already has image metadata.
func HasImageMetadata(database *db.DB, fileID int64) bool {
	var id int64
	row := database.QueryRow("SELECT id FROM image_metadata WHERE file_id = ?", fileID)
	return row.Scan(&id) == nil
}
