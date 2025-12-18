package tile

// Output format constants
const (
	OUTFMT_PNG = iota
	OUTFMT_GEOTIFF
)

// ImageData holds decoded image data
type ImageData struct {
	Buf    []byte
	Width  int
	Height int
	Depth  int // channels: 1=grayscale, 3=RGB, 4=RGBA
}

// StitchOptions contains all configuration for tile stitching
type StitchOptions struct {
	Output         string
	TileSize       int
	Centered       bool
	Format         int
	WriteWorldFile bool
	UserAgent      string
}

// BoundingBox represents geographic bounds
type BoundingBox struct {
	MinLat, MinLon, MaxLat, MaxLon float64
}

// CenteredRequest represents a centered tile request
type CenteredRequest struct {
	Lat, Lon          float64
	Width, Height     int
}