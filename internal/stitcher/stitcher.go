package stitcher

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Output format constants
const (
	FormatPNG = iota
	FormatGeoTIFF
)

// Mode constants
const (
	ModeBBox = iota
	ModeCentered
)

// Options contains all stitching parameters
type Options struct {
	// Coordinates for bbox mode
	MinLat, MinLon, MaxLat, MaxLon float64
	
	// Coordinates for centered mode
	CenterLat, CenterLon float64
	Width, Height        int
	
	// Common options
	Zoom              int
	TileURLs          []string
	TileSize          int
	OutputFormat      int
	GenerateWorldFile bool
	Headers           map[string]string
	Mode              int
}

// Result contains the stitching result
type Result struct {
	ImageData     []byte
	WorldFileData []byte
	Width         int
	Height        int
	MinX, MaxY    float64 // For world file
	PixelSizeX    float64
	PixelSizeY    float64
}

// TileError represents errors related to tile downloading
type TileError struct {
	Message         string
	FailedTiles     []FailedTile
	SuccessfulTiles int
	TotalTiles      int
}

func (e *TileError) Error() string {
	return e.Message
}

// FailedTile represents a single failed tile download
type FailedTile struct {
	URL        string
	StatusCode *int
	Error      string
}

// ImageData holds decoded image information
type ImageData struct {
	buf    []byte
	width  int
	height int
	depth  int // channels: 1=grayscale, 3=RGB, 4=RGBA
}

// Stitcher performs tile stitching operations
type Stitcher struct {
	client *http.Client
}

// New creates a new stitcher instance
func New() *Stitcher {
	return &Stitcher{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Stitch performs the tile stitching operation
func (s *Stitcher) Stitch(ctx context.Context, opts *Options) (*Result, error) {
	// Calculate tile coordinates and bounds
	var x1, y1, x2, y2 uint32
	var minLat, minLon, maxLat, maxLon float64
	
	if opts.Mode == ModeCentered {
		// Convert centered mode to bounding box
		cx, cy := latlon2tile(opts.CenterLat, opts.CenterLon, 32)
		
		x1 = cx - uint32((opts.Width<<(32-(opts.Zoom+8)))/2)
		y1 = cy - uint32((opts.Height<<(32-(opts.Zoom+8)))/2)
		x2 = cx + uint32((opts.Width<<(32-(opts.Zoom+8)))/2)
		y2 = cy + uint32((opts.Height<<(32-(opts.Zoom+8)))/2)
		
		maxLat, minLon = tile2latlon(x1, y1, 32)
		minLat, maxLon = tile2latlon(x2, y2, 32)
	} else {
		// Bounding box mode
		minLat, minLon, maxLat, maxLon = opts.MinLat, opts.MinLon, opts.MaxLat, opts.MaxLon
		x1, y1 = latlon2tile(maxLat, minLon, 32)
		x2, y2 = latlon2tile(minLat, maxLon, 32)
	}
	
	// Convert to actual tile coordinates
	tx1 := x1 >> (32 - opts.Zoom)
	ty1 := y1 >> (32 - opts.Zoom)
	tx2 := x2 >> (32 - opts.Zoom)
	ty2 := y2 >> (32 - opts.Zoom)
	
	// Calculate pixel offsets and dimensions
	xa := int(((x1 >> (32 - (opts.Zoom + 8))) & 0xFF) * uint32(opts.TileSize) / 256)
	ya := int(((y1 >> (32 - (opts.Zoom + 8))) & 0xFF) * uint32(opts.TileSize) / 256)
	
	width := int(((x2 >> (32 - (opts.Zoom + 8))) - (x1 >> (32 - (opts.Zoom + 8)))) * uint32(opts.TileSize) / 256)
	height := int(((y2 >> (32 - (opts.Zoom + 8))) - (y1 >> (32 - (opts.Zoom + 8)))) * uint32(opts.TileSize) / 256)
	
	// Check size limits
	dim := int64(width) * int64(height)
	if dim > 10000*10000 {
		return nil, fmt.Errorf("requested image size too large: %dx%d", width, height)
	}
	
	// Project coordinates for world file
	minX, minY := projectlatlon(minLat, minLon)
	maxX, maxY := projectlatlon(maxLat, maxLon)
	
	px := (maxX - minX) / float64(width)
	py := math.Abs(maxY-minY) / float64(height)
	
	// Allocate output buffer
	buf := make([]byte, width*height*4)
	
	// Track tile download statistics
	var failedTiles []FailedTile
	successfulTiles := 0
	totalTiles := int((tx2-tx1+1) * (ty2-ty1+1) * uint32(len(opts.TileURLs)))
	
	// Download and stitch tiles
	for ty := ty1; ty <= ty2; ty++ {
		for tx := tx1; tx <= tx2; tx++ {
			xoff := int(tx-tx1)*opts.TileSize - xa
			yoff := int(ty-ty1)*opts.TileSize - ya
			
			tileProcessed := false
			for _, urlTemplate := range opts.TileURLs {
				url := s.buildURL(urlTemplate, opts.Zoom, tx, ty)
				
				// Check context cancellation
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				default:
				}
				
				data, err := s.downloadTile(ctx, url, opts.Headers)
				if err != nil {
					failedTiles = append(failedTiles, FailedTile{
						URL:   url,
						Error: err.Error(),
					})
					continue
				}
				
				img, err := s.decodeImage(data)
				if err != nil {
					failedTiles = append(failedTiles, FailedTile{
						URL:   url,
						Error: fmt.Sprintf("decode error: %v", err),
					})
					continue
				}
				
				if img.height != opts.TileSize || img.width != opts.TileSize {
					failedTiles = append(failedTiles, FailedTile{
						URL:   url,
						Error: fmt.Sprintf("wrong tile size: got %dx%d, expected %dx%d", img.width, img.height, opts.TileSize, opts.TileSize),
					})
					continue
				}
				
				// Copy tile data to output buffer
				s.copyTileToBuffer(img, buf, xoff, yoff, width, height)
				successfulTiles++
				tileProcessed = true
				break // Successfully processed this tile position
			}
			
			if !tileProcessed {
				// All URLs failed for this tile position
				continue
			}
		}
	}
	
	// Check if we have enough successful tiles
	if successfulTiles == 0 {
		return nil, &TileError{
			Message:         "No tiles could be downloaded successfully",
			FailedTiles:     failedTiles,
			SuccessfulTiles: successfulTiles,
			TotalTiles:      totalTiles,
		}
	}
	
	// If more than 50% of tiles failed, return a tile error
	if len(failedTiles) > totalTiles/2 {
		return nil, &TileError{
			Message:         fmt.Sprintf("Too many tile download failures: %d/%d failed", len(failedTiles), totalTiles),
			FailedTiles:     failedTiles,
			SuccessfulTiles: successfulTiles,
			TotalTiles:      totalTiles,
		}
	}
	
	// Encode output image
	var imageData []byte
	var err error
	
	switch opts.OutputFormat {
	case FormatPNG:
		imageData, err = s.encodePNG(buf, width, height)
	case FormatGeoTIFF:
		return nil, fmt.Errorf("GeoTIFF output not yet implemented")
	default:
		imageData, err = s.encodePNG(buf, width, height)
	}
	
	if err != nil {
		return nil, fmt.Errorf("failed to encode output image: %v", err)
	}
	
	result := &Result{
		ImageData:  imageData,
		Width:      width,
		Height:     height,
		MinX:       minX,
		MaxY:       maxY,
		PixelSizeX: px,
		PixelSizeY: py,
	}
	
	// Generate world file if requested
	if opts.GenerateWorldFile {
		result.WorldFileData = s.generateWorldFile(px, py, minX, maxY)
	}
	
	return result, nil
}

// downloadTile downloads a single tile
func (s *Stitcher) downloadTile(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	// Set User-Agent
	req.Header.Set("User-Agent", "tile-stitch/2.0.0")
	
	// Set additional headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}
	
	return io.ReadAll(resp.Body)
}

// decodeImage decodes an image from bytes
func (s *Stitcher) decodeImage(data []byte) (*ImageData, error) {
	if len(data) >= 4 && bytes.Equal(data[:4], []byte{0x89, 0x50, 0x4E, 0x47}) {
		return s.readPNG(data)
	} else if len(data) >= 2 && bytes.Equal(data[:2], []byte{0xFF, 0xD8}) {
		return s.readJPEG(data)
	}
	
	return nil, fmt.Errorf("unrecognized image format")
}

// readPNG decodes a PNG image
func (s *Stitcher) readPNG(data []byte) (*ImageData, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	
	return s.imageToImageData(img), nil
}

// readJPEG decodes a JPEG image
func (s *Stitcher) readJPEG(data []byte) (*ImageData, error) {
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	
	return s.imageToImageData(img), nil
}

// imageToImageData converts a Go image to ImageData
func (s *Stitcher) imageToImageData(img image.Image) *ImageData {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	
	// Convert to RGBA
	buf := make([]byte, width*height*4)
	
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			idx := (y*width + x) * 4
			buf[idx] = byte(r >> 8)
			buf[idx+1] = byte(g >> 8)
			buf[idx+2] = byte(b >> 8)
			buf[idx+3] = byte(a >> 8)
		}
	}
	
	return &ImageData{
		buf:    buf,
		width:  width,
		height: height,
		depth:  4,
	}
}

// copyTileToBuffer copies tile data to the output buffer
func (s *Stitcher) copyTileToBuffer(img *ImageData, buf []byte, xoff, yoff, width, height int) {
	for y := 0; y < img.height; y++ {
		for x := 0; x < img.width; x++ {
			xd := x + xoff
			yd := y + yoff
			
			if xd < 0 || yd < 0 || xd >= width || yd >= height {
				continue
			}
			
			srcIdx := (y*img.width + x) * 4
			dstIdx := (yd*width + xd) * 4
			
			// Alpha blending
			src := [4]byte{img.buf[srcIdx], img.buf[srcIdx+1], img.buf[srcIdx+2], img.buf[srcIdx+3]}
			dst := [4]byte{buf[dstIdx], buf[dstIdx+1], buf[dstIdx+2], buf[dstIdx+3]}
			result := s.alphaBlend(src, dst)
			copy(buf[dstIdx:dstIdx+4], result[:])
		}
	}
}

// alphaBlend performs alpha blending of two pixels
func (s *Stitcher) alphaBlend(src, dst [4]byte) [4]byte {
	as := float64(src[3]) / 255.0
	rs := float64(src[0]) / 255.0 * as
	gs := float64(src[1]) / 255.0 * as
	bs := float64(src[2]) / 255.0 * as
	
	ad := float64(dst[3]) / 255.0
	rd := float64(dst[0]) / 255.0 * ad
	gd := float64(dst[1]) / 255.0 * ad
	bd := float64(dst[2]) / 255.0 * ad
	
	// Alpha compositing
	ar := as*(1-ad) + ad
	rr := rs*(1-ad) + rd
	gr := gs*(1-ad) + gd
	br := bs*(1-ad) + bd
	
	if ar > 0 {
		return [4]byte{
			byte(rr / ar * 255.0),
			byte(gr / ar * 255.0),
			byte(br / ar * 255.0),
			byte(ar * 255.0),
		}
	}
	
	return [4]byte{0, 0, 0, 0}
}

// encodePNG encodes the buffer as PNG
func (s *Stitcher) encodePNG(buf []byte, width, height int) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	copy(img.Pix, buf)
	
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		return nil, err
	}
	
	return output.Bytes(), nil
}

// generateWorldFile generates world file data
func (s *Stitcher) generateWorldFile(px, py, minx, maxy float64) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%24.10f\n", px)
	fmt.Fprintf(&buf, "%24.10f\n", 0.0)
	fmt.Fprintf(&buf, "%24.10f\n", 0.0)
	fmt.Fprintf(&buf, "%24.10f\n", -py)
	fmt.Fprintf(&buf, "%24.10f\n", minx)
	fmt.Fprintf(&buf, "%24.10f\n", maxy)
	return buf.Bytes()
}

// buildURL replaces URL template tokens
func (s *Stitcher) buildURL(template string, zoom int, x, y uint32) string {
	url := template
	url = strings.ReplaceAll(url, "{z}", strconv.Itoa(zoom))
	url = strings.ReplaceAll(url, "{x}", strconv.FormatUint(uint64(x), 10))
	url = strings.ReplaceAll(url, "{y}", strconv.FormatUint(uint64(y), 10))
	// Handle {s} for subdomains (simple implementation)
	if strings.Contains(url, "{s}") {
		subdomain := string(rune('a' + (x+y)%3))
		url = strings.ReplaceAll(url, "{s}", subdomain)
	}
	return url
}

// Coordinate conversion functions

// latlon2tile converts lat/lon to tile coordinates at given zoom level
func latlon2tile(lat, lon float64, zoom int) (uint32, uint32) {
	latRad := lat * math.Pi / 180
	n := uint64(1) << uint(zoom)
	
	x := uint32(float64(n) * ((lon + 180) / 360))
	y := uint32(float64(n) * (1 - (math.Log(math.Tan(latRad)+1/math.Cos(latRad))/math.Pi)) / 2)
	
	return x, y
}

// tile2latlon converts tile coordinates to lat/lon
func tile2latlon(x, y uint32, zoom int) (float64, float64) {
	n := float64(uint64(1) << uint(zoom))
	lon := 360.0*float64(x)/n - 180.0
	latRad := math.Atan(math.Sinh(math.Pi * (1 - 2.0*float64(y)/n)))
	lat := latRad * 180 / math.Pi
	
	return lat, lon
}

// projectlatlon converts lat/lon in WGS84 to XY in Spherical Mercator (EPSG:900913/3857)
func projectlatlon(lat, lon float64) (float64, float64) {
	const originshift = 20037508.342789244 // 2 * pi * 6378137 / 2
	x := lon * originshift / 180.0
	y := math.Log(math.Tan((90+lat)*math.Pi/360.0)) / (math.Pi / 180.0)
	y = y * originshift / 180.0
	
	return x, y
}