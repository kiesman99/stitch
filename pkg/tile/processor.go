package tile

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// Processor handles tile downloading and processing
type Processor struct {
	client    *http.Client
	userAgent string
}

// NewProcessor creates a new tile processor
func NewProcessor(userAgent string) *Processor {
	return &Processor{
		client:    &http.Client{},
		userAgent: userAgent,
	}
}

// LatLonToTile converts lat/lon to tile coordinates at given zoom level
// http://wiki.openstreetmap.org/wiki/Slippy_map_tilenames
func LatLonToTile(lat, lon float64, zoom int) (uint32, uint32) {
	latRad := lat * math.Pi / 180
	n := uint64(1) << uint(zoom)
	
	x := uint32(float64(n) * ((lon + 180) / 360))
	y := uint32(float64(n) * (1 - (math.Log(math.Tan(latRad)+1/math.Cos(latRad))/math.Pi)) / 2)
	
	return x, y
}

// TileToLatLon converts tile coordinates to lat/lon
func TileToLatLon(x, y uint32, zoom int) (float64, float64) {
	n := float64(uint64(1) << uint(zoom))
	lon := 360.0*float64(x)/n - 180.0
	latRad := math.Atan(math.Sinh(math.Pi * (1 - 2.0*float64(y)/n)))
	lat := latRad * 180 / math.Pi
	
	return lat, lon
}

// ProjectLatLon converts lat/lon in WGS84 to XY in Spherical Mercator (EPSG:900913/3857)
func ProjectLatLon(lat, lon float64) (float64, float64) {
	const originshift = 20037508.342789244 // 2 * pi * 6378137 / 2
	x := lon * originshift / 180.0
	y := math.Log(math.Tan((90+lat)*math.Pi/360.0)) / (math.Pi / 180.0)
	y = y * originshift / 180.0
	
	return x, y
}

// DownloadTile downloads a tile from the given URL
func (p *Processor) DownloadTile(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("User-Agent", p.userAgent)
	
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}
	
	return io.ReadAll(resp.Body)
}

// DecodeImage detects image format and decodes
func (p *Processor) DecodeImage(data []byte) (*ImageData, error) {
	if len(data) >= 4 && bytes.Equal(data[:4], []byte{0x89, 0x50, 0x4E, 0x47}) {
		return p.readPNG(data)
	} else if len(data) >= 2 && bytes.Equal(data[:2], []byte{0xFF, 0xD8}) {
		return p.readJPEG(data)
	}
	
	return nil, fmt.Errorf("unrecognized image format")
}

// readJPEG decodes JPEG image
func (p *Processor) readJPEG(data []byte) (*ImageData, error) {
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	
	// Convert to RGBA - JPEG doesn't have alpha, so we'll use RGB with full alpha
	buf := make([]byte, width*height*4)
	
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			idx := (y*width + x) * 4
			buf[idx] = byte(r >> 8)     // R
			buf[idx+1] = byte(g >> 8)   // G
			buf[idx+2] = byte(b >> 8)   // B
			buf[idx+3] = 255            // A (full opacity for JPEG)
		}
	}
	
	return &ImageData{
		Buf:    buf,
		Width:  width,
		Height: height,
		Depth:  3, // JPEG is RGB, not RGBA
	}, nil
}

// readPNG decodes PNG image
func (p *Processor) readPNG(data []byte) (*ImageData, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	
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
		Buf:    buf,
		Width:  width,
		Height: height,
		Depth:  4,
	}, nil
}

// BuildURL replaces URL template tokens
func BuildURL(template string, zoom int, x, y uint32) string {
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

// AlphaBlend blends two pixels with alpha compositing
func AlphaBlend(src, dst [4]byte) [4]byte {
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

// WritePNG writes PNG output
func WritePNG(filename string, buf []byte, width, height int) error {
	var output io.Writer
	
	if filename == "" {
		output = os.Stdout
		fmt.Fprintf(os.Stderr, "Output PNG: stdout\n")
	} else {
		fmt.Fprintf(os.Stderr, "Output PNG: %s\n", filename)
		file, err := os.Create(filename)
		if err != nil {
			return err
		}
		defer file.Close()
		output = file
	}
	
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	copy(img.Pix, buf)
	
	return png.Encode(output, img)
}

// WriteWorldFile writes world file
func WriteWorldFile(filename string, px, py, minx, maxy float64, outfmt int) error {
	if filename == "" {
		return fmt.Errorf("can't write a worldfile when writing to stdout")
	}
	
	var ext string
	if outfmt == OUTFMT_PNG {
		ext = ".pnw"
	} else {
		ext = ".tfw"
	}
	
	// Replace extension
	worldFilename := filename
	if idx := strings.LastIndex(worldFilename, "."); idx != -1 {
		worldFilename = worldFilename[:idx] + ext
	} else {
		worldFilename += ext
	}
	
	file, err := os.Create(worldFilename)
	if err != nil {
		return err
	}
	defer file.Close()
	
	// World file format: pixel size x, rotation, rotation, pixel size y (negative), top left x, top left y
	fmt.Fprintf(file, "%24.10f\n", px)
	fmt.Fprintf(file, "%24.10f\n", 0.0)
	fmt.Fprintf(file, "%24.10f\n", 0.0)
	fmt.Fprintf(file, "%24.10f\n", -py)
	fmt.Fprintf(file, "%24.10f\n", minx)
	fmt.Fprintf(file, "%24.10f\n", maxy)
	
	fmt.Fprintf(os.Stderr, "World file written to '%s'.\n", worldFilename)
	return nil
}