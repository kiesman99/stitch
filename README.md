# stitch

> This project is highly inspired by [tile-stitch](https://github.com/e-n-f/tile-stitch/tree/master). 

Stitch together and crop map tiles for any bounding box.

The tiles should come from a web map service in PNG or JPEG format, and will be written out as PNG or a georeferenced TIFF.

## Installation

```bash
go build -o stitch .
```

## Usage

### Basic Examples

**Self-documenting tile stitching with named flags:**
```bash
# Get standard OpenStreetMap tiles at zoom level 10 for a bounding box (explicit coordinates)
./stitch --min-lat 37.371794 --min-lon -122.917099 --max-lat 38.226853 --max-lon -121.564407 --zoom 10 --url "http://a.tile.openstreetmap.org/{z}/{x}/{y}.png" -o baymodel.png

# Same thing using the compact bbox format
./stitch --bbox 37.371794,-122.917099,38.226853,-121.564407 --zoom 10 --url "http://a.tile.openstreetmap.org/{z}/{x}/{y}.png" -o baymodel.png

# Get tiles with world file
./stitch --bbox 37.37,-122.92,38.23,-121.56 --zoom 10 --url "http://a.tile.openstreetmap.org/{z}/{x}/{y}.png" -w -o baymodel.png

# Get a 640x480 image centered around Tokyo
./stitch --lat 35.6824 --lon 139.7531 --width 640 --height 480 --zoom 10 --url "http://b.tile.stamen.com/watercolor/{z}/{x}/{y}.jpg" -o tokyo.png

# Get an image using 512x512 retina tiles around Köln
./stitch --min-lat 50.88 --min-lon 6.88 --max-lat 50.98 --max-lon 7.04 --zoom 14 --tilesize 512 --url "http://b.tile.stamen.com/toner/{z}/{x}/{y}@2x.png" -o köln.png

# Multiple tile sources for redundancy
./stitch --bbox 37.37,-122.92,38.23,-121.56 --zoom 10 --url "http://a.tile.openstreetmap.org/{z}/{x}/{y}.png" --url "http://b.tile.openstreetmap.org/{z}/{x}/{y}.png" -o map.png
```

**HTTP Server:**
```bash
# Start HTTP server on default port 8080
./stitch serve

# Start server on custom port
./stitch serve --port 3000

# Start server with custom bind address
./stitch serve --bind 0.0.0.0 --port 8080
```

### Command Structure

The CLI uses Cobra and Viper with **self-documenting named flags** instead of confusing positional arguments:

```bash
stitch [coordinate-flags] --zoom <level> --url <template> [other-flags]    # Default stitching
stitch serve [flags]                                                       # HTTP server
```

### Coordinate Modes

**Bounding Box Mode (geographic bounds):**
```bash
# Individual coordinate flags (most explicit)
stitch --min-lat <south> --min-lon <west> --max-lat <north> --max-lon <east> --zoom <level> --url <template>

# Compact bbox format
stitch --bbox <min-lat,min-lon,max-lat,max-lon> --zoom <level> --url <template>
```

**Centered Mode (point + dimensions):**
```bash
stitch --lat <center-lat> --lon <center-lon> --width <pixels> --height <pixels> --zoom <level> --url <template>
```

### Flags

**Required flags:**
- `--zoom`: Zoom level (required)
- `--url, -u`: Tile URL template(s) with {z}, {x}, {y} placeholders (required, can be specified multiple times)

**Coordinate flags (choose one mode):**
- `--min-lat, --min-lon, --max-lat, --max-lon`: Individual bounding box coordinates
- `--bbox`: Compact bounding box as 'min-lat,min-lon,max-lat,max-lon'
- `--lat, --lon, --width, --height`: Centered mode coordinates

**Output flags:**
- `-o, --output`: Output file (default: stdout)
- `-f, --format`: Output format (png|geotiff)
- `-w, --worldfile`: Write world file
- `-t, --tilesize`: Tile size in pixels (default: 256)
- `--user-agent`: HTTP User-Agent header
- `--config`: Config file (default: $HOME/.stitch.yaml)

**Server flags:**
- `-b, --bind`: Bind address (default: localhost)
- `-p, --port`: Port to listen on (default: 8080)
- `--timeout`: Request timeout (default: 30s)

### Configuration

You can use a configuration file to set default values. Copy `.stitch.yaml.example` to `~/.stitch.yaml` or specify with `--config`.

Example config:
```yaml
format: "png"
tilesize: 256
user-agent: "stitch/2.0.0"
server:
  bind: "localhost"
  port: 8080
  timeout: "30s"
```

## Behavior

- **`stitch <args>`**: Directly performs tile stitching (default behavior)
- **`stitch serve`**: Starts HTTP server for API access
- **`stitch --help`**: Shows help and available commands

This design makes the CLI intuitive - most users will just run `stitch` with their parameters, while `stitch serve` provides API access when needed.

## Format

The arguments are `minlat minlon maxlat maxlon zoom url`. If you don't specify `-o outfile` the PNG will be written to the standard output. URLs should include `{z}, {x},` and `{y}` tokens for tile zoom, x, and y.

## Restrictions

- GeoTIFF is currently only supported when an output filename is specified
- A worldfile cannot be generated unless an output filename is specified
- GeoTIFF output is not yet fully implemented (falls back to PNG with warning)

## Requirements

This Go version has no external system dependencies - everything is handled by Go's standard library and the imported packages.

## Development

Codegen the rest server via:

```sh
go generate ./internal/api/generate.go
```