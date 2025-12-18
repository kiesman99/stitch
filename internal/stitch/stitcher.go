package stitch

import (
	"fmt"
	"math"
	"os"

	"github.com/kiesman99/stitch/pkg/tile"
)

// Stitcher handles the main stitching logic
type Stitcher struct {
	processor *tile.Processor
	options   *tile.StitchOptions
}

// NewStitcher creates a new stitcher instance
func NewStitcher(opts *tile.StitchOptions) *Stitcher {
	userAgent := opts.UserAgent
	if userAgent == "" {
		userAgent = "stitch/2.0.0"
	}

	return &Stitcher{
		processor: tile.NewProcessor(userAgent),
		options:   opts,
	}
}

// StitchBoundingBox stitches tiles for a geographic bounding box
func (s *Stitcher) StitchBoundingBox(bbox *tile.BoundingBox, zoom int, urls []string) error {
	return s.stitch(bbox.MinLat, bbox.MinLon, bbox.MaxLat, bbox.MaxLon, zoom, urls, false, 0, 0)
}

// StitchCentered stitches tiles for a centered request
func (s *Stitcher) StitchCentered(req *tile.CenteredRequest, zoom int, urls []string) error {
	return s.stitch(req.Lat, req.Lon, 0, 0, zoom, urls, true, req.Width, req.Height)
}

func (s *Stitcher) stitch(minlat, minlon, maxlat, maxlon float64, zoom int, urls []string, centered bool, width, height int) error {
	if zoom < 0 {
		return fmt.Errorf("zoom %d less than 0", zoom)
	}

	if len(urls) == 0 {
		return fmt.Errorf("no tile URLs provided")
	}

	// Check if output is to terminal
	if s.options.Output == "" {
		if stat, _ := os.Stdout.Stat(); (stat.Mode() & os.ModeCharDevice) != 0 {
			return fmt.Errorf("didn't specify output file and standard output is a terminal")
		}
	}

	var x1, y1, x2, y2 uint32

	if centered {
		lat := minlat
		lon := minlon

		if width <= 0 || height <= 0 {
			return fmt.Errorf("width/height less than 0: %d %d", width, height)
		}

		// Calculate tile coordinates at high precision
		cx, cy := tile.LatLonToTile(lat, lon, 32)

		// Calculate bounds
		x1 = cx - uint32((width<<(32-(zoom+8)))/2)
		y1 = cy - uint32((height<<(32-(zoom+8)))/2)
		x2 = cx + uint32((width<<(32-(zoom+8)))/2)
		y2 = cy + uint32((height<<(32-(zoom+8)))/2)

		// Convert back to lat/lon
		maxlat, minlon = tile.TileToLatLon(x1, y1, 32)
		minlat, maxlon = tile.TileToLatLon(x2, y2, 32)
	} else {
		// Bounding box mode
		x1, y1 = tile.LatLonToTile(maxlat, minlon, 32)
		x2, y2 = tile.LatLonToTile(minlat, maxlon, 32)
	}

	// Convert to actual tile coordinates
	tx1 := x1 >> (32 - zoom)
	ty1 := y1 >> (32 - zoom)
	tx2 := x2 >> (32 - zoom)
	ty2 := y2 >> (32 - zoom)

	// Project coordinates
	minx, miny := tile.ProjectLatLon(minlat, minlon)
	maxx, maxy := tile.ProjectLatLon(maxlat, maxlon)

	fmt.Fprintf(os.Stderr, "==Geodetic Bounds  (EPSG:4236): %.17g,%.17g to %.17g,%.17g\n", minlat, minlon, maxlat, maxlon)
	fmt.Fprintf(os.Stderr, "==Projected Bounds (EPSG:3785): %.17g,%.17g to %.17g,%.17g\n", miny, minx, maxy, maxx)
	fmt.Fprintf(os.Stderr, "==Zoom Level: %d\n", zoom)
	fmt.Fprintf(os.Stderr, "==Upper Left Tile: x:%d y:%d\n", tx1, ty2)
	fmt.Fprintf(os.Stderr, "==Lower Right Tile: x:%d y:%d\n", tx2, ty1)

	// Calculate pixel offsets and dimensions
	xa := int(((x1 >> (32 - (zoom + 8))) & 0xFF) * uint32(s.options.TileSize) / 256)
	ya := int(((y1 >> (32 - (zoom + 8))) & 0xFF) * uint32(s.options.TileSize) / 256)

	outputWidth := int(((x2 >> (32 - (zoom + 8))) - (x1 >> (32 - (zoom + 8)))) * uint32(s.options.TileSize) / 256)
	outputHeight := int(((y2 >> (32 - (zoom + 8))) - (y1 >> (32 - (zoom + 8)))) * uint32(s.options.TileSize) / 256)

	fmt.Fprintf(os.Stderr, "==Raster Size: %dx%d\n", outputWidth, outputHeight)

	px := (maxx - minx) / float64(outputWidth)
	py := math.Abs(maxy-miny) / float64(outputHeight)
	fmt.Fprintf(os.Stderr, "==Pixel Size: x:%.17g y:%.17g\n", px, py)

	// Check size limits
	dim := int64(outputWidth) * int64(outputHeight)
	if dim > 10000*10000 {
		return fmt.Errorf("that's too big")
	}

	// Allocate output buffer
	buf := make([]byte, outputWidth*outputHeight*4)

	// Download and stitch tiles
	for ty := ty1; ty <= ty2; ty++ {
		for tx := tx1; tx <= tx2; tx++ {
			progress := (float64(ty-ty1)/float64((ty2+1)-ty1) +
				float64(tx-tx1)/float64((ty2+1)-ty1)/float64((tx2+1)-tx1)) * 100

			xoff := int(tx-tx1)*s.options.TileSize - int(xa)
			yoff := int(ty-ty1)*s.options.TileSize - int(ya)

			for _, urlTemplate := range urls {
				url := tile.BuildURL(urlTemplate, zoom, tx, ty)
				fmt.Fprintf(os.Stderr, "%.2f%%: %s\n", progress, url)

				data, err := s.processor.DownloadTile(url)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Can't retrieve %s: %v\n", url, err)
					continue
				}

				img, err := s.processor.DecodeImage(data)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Can't decode image from %s: %v\n", url, err)
					continue
				}

				if img.Height != s.options.TileSize || img.Width != s.options.TileSize {
					fmt.Fprintf(os.Stderr, "Got %dx%d tile, not %d\n", img.Width, img.Height, s.options.TileSize)
					continue
				}

				// Copy tile data to output buffer
				for y := 0; y < img.Height; y++ {
					for x := 0; x < img.Width; x++ {
						xd := x + xoff
						yd := y + yoff

						if xd < 0 || yd < 0 || xd >= outputWidth || yd >= outputHeight {
							continue
						}

						srcIdx := (y*img.Width + x) * 4
						dstIdx := (yd*outputWidth + xd) * 4

						if img.Depth == 4 {
							// Alpha blending
							src := [4]byte{img.Buf[srcIdx], img.Buf[srcIdx+1], img.Buf[srcIdx+2], img.Buf[srcIdx+3]}
							dst := [4]byte{buf[dstIdx], buf[dstIdx+1], buf[dstIdx+2], buf[dstIdx+3]}
							result := tile.AlphaBlend(src, dst)
							copy(buf[dstIdx:dstIdx+4], result[:])
						} else if img.Depth == 3 {
							// RGB
							buf[dstIdx] = img.Buf[srcIdx]
							buf[dstIdx+1] = img.Buf[srcIdx+1]
							buf[dstIdx+2] = img.Buf[srcIdx+2]
							buf[dstIdx+3] = 255
						} else {
							// Grayscale
							val := img.Buf[srcIdx]
							buf[dstIdx] = val
							buf[dstIdx+1] = val
							buf[dstIdx+2] = val
							buf[dstIdx+3] = 255
						}
					}
				}
			}
		}
	}

	// Write output
	if s.options.Format == tile.OUTFMT_PNG {
		if err := tile.WritePNG(s.options.Output, buf, outputWidth, outputHeight); err != nil {
			return fmt.Errorf("failed to write PNG: %v", err)
		}
	} else if s.options.Format == tile.OUTFMT_GEOTIFF {
		return fmt.Errorf("GeoTIFF output not yet implemented")
	}

	// Write world file if requested
	if s.options.WriteWorldFile {
		if err := tile.WriteWorldFile(s.options.Output, px, py, minx, maxy, s.options.Format); err != nil {
			return fmt.Errorf("failed to write world file: %v", err)
		}
	}

	return nil
}
