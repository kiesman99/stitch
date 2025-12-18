package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/kiesman99/stitch/internal/stitch"
	"github.com/kiesman99/stitch/pkg/tile"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "stitch",
	Short: "Stitch together and crop map tiles for any bounding box",
	Long: `stitch downloads and stitches together map tiles from web map services.

The tiles should come from a web map service in PNG or JPEG format, and will be 
written out as PNG or a georeferenced TIFF. Optionally, a separate worldfile 
with georeferencing data can be written.

Examples:
  # Get OpenStreetMap tiles at zoom level 10 (bounding box mode)
  stitch --min-lat 37.371794 --min-lon -122.917099 --max-lat 38.226853 --max-lon -121.564407 --zoom 10 --url http://a.tile.openstreetmap.org/{z}/{x}/{y}.png -o baymodel.png

  # Get tiles with GeoTIFF output and world file
  stitch --bbox 37.371794,-122.917099,38.226853,-121.564407 --zoom 10 --url http://a.tile.openstreetmap.org/{z}/{x}/{y}.png -f geotiff -w -o baymodel.tif

  # Get centered image around Tokyo
  stitch --lat 35.6824 --lon 139.7531 --width 640 --height 480 --zoom 10 --url http://b.tile.stamen.com/watercolor/{z}/{x}/{y}.jpg -o tokyo.png

  # Multiple tile sources
  stitch --bbox 37.37,-122.92,38.23,-121.56 --zoom 10 --url http://a.tile.openstreetmap.org/{z}/{x}/{y}.png --url http://b.tile.openstreetmap.org/{z}/{x}/{y}.png -o map.png

  # Start HTTP server
  stitch serve --port 8080`,
	// If no subcommand is specified and we have args, run the stitch command
	RunE: func(cmd *cobra.Command, args []string) error {
		// If no args, show help
		if len(args) == 0 {
			return cmd.Help()
		}
		// Otherwise, delegate to stitch command
		return runStitch(cmd, args)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.stitch.yaml)")

	// Add stitch command flags to root for default behavior
	// Output options
	rootCmd.Flags().StringP("output", "o", "", "output file (default: stdout)")
	rootCmd.Flags().StringP("format", "f", "png", "output format (png|geotiff)")
	rootCmd.Flags().BoolP("worldfile", "w", false, "write world file")
	
	// Coordinate options - Bounding box mode
	rootCmd.Flags().Float64("min-lat", 0, "minimum latitude (south boundary)")
	rootCmd.Flags().Float64("min-lon", 0, "minimum longitude (west boundary)")
	rootCmd.Flags().Float64("max-lat", 0, "maximum latitude (north boundary)")
	rootCmd.Flags().Float64("max-lon", 0, "maximum longitude (east boundary)")
	rootCmd.Flags().String("bbox", "", "bounding box as 'min-lat,min-lon,max-lat,max-lon'")
	
	// Coordinate options - Centered mode
	rootCmd.Flags().Float64("lat", 0, "center latitude")
	rootCmd.Flags().Float64("lon", 0, "center longitude")
	rootCmd.Flags().Int("width", 0, "image width in pixels (centered mode)")
	rootCmd.Flags().Int("height", 0, "image height in pixels (centered mode)")
	
	// Tile options
	rootCmd.Flags().Int("zoom", 0, "zoom level (required)")
	rootCmd.Flags().StringSliceP("url", "u", []string{}, "tile URL template(s) with {z}, {x}, {y} placeholders (required)")
	rootCmd.Flags().IntP("tilesize", "t", 256, "tile size in pixels")
	
	// HTTP options
	rootCmd.Flags().String("user-agent", "stitch/2.0.0", "HTTP User-Agent header")
	
	// Bind flags to viper for root command
	viper.BindPFlag("output", rootCmd.Flags().Lookup("output"))
	viper.BindPFlag("format", rootCmd.Flags().Lookup("format"))
	viper.BindPFlag("worldfile", rootCmd.Flags().Lookup("worldfile"))
	viper.BindPFlag("min-lat", rootCmd.Flags().Lookup("min-lat"))
	viper.BindPFlag("min-lon", rootCmd.Flags().Lookup("min-lon"))
	viper.BindPFlag("max-lat", rootCmd.Flags().Lookup("max-lat"))
	viper.BindPFlag("max-lon", rootCmd.Flags().Lookup("max-lon"))
	viper.BindPFlag("bbox", rootCmd.Flags().Lookup("bbox"))
	viper.BindPFlag("lat", rootCmd.Flags().Lookup("lat"))
	viper.BindPFlag("lon", rootCmd.Flags().Lookup("lon"))
	viper.BindPFlag("width", rootCmd.Flags().Lookup("width"))
	viper.BindPFlag("height", rootCmd.Flags().Lookup("height"))
	viper.BindPFlag("zoom", rootCmd.Flags().Lookup("zoom"))
	viper.BindPFlag("url", rootCmd.Flags().Lookup("url"))
	viper.BindPFlag("tilesize", rootCmd.Flags().Lookup("tilesize"))
	viper.BindPFlag("user-agent", rootCmd.Flags().Lookup("user-agent"))
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".stitch" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".stitch")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}

func runStitch(cmd *cobra.Command, args []string) error {
	// Validate required parameters
	zoom := viper.GetInt("zoom")
	urls := viper.GetStringSlice("url")
	
	if zoom == 0 {
		return fmt.Errorf("zoom level is required (use --zoom)")
	}
	
	if len(urls) == 0 {
		return fmt.Errorf("at least one tile URL is required (use --url)")
	}

	// Parse format
	formatStr := viper.GetString("format")
	var format int
	switch formatStr {
	case "png":
		format = tile.OUTFMT_PNG
	case "geotiff":
		format = tile.OUTFMT_GEOTIFF
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: GeoTIFF output not yet implemented, using PNG\n")
		format = tile.OUTFMT_PNG
	default:
		return fmt.Errorf("unknown format: %s", formatStr)
	}

	// Determine mode based on provided flags
	bbox := viper.GetString("bbox")
	minLat := viper.GetFloat64("min-lat")
	maxLat := viper.GetFloat64("max-lat")
	minLon := viper.GetFloat64("min-lon")
	maxLon := viper.GetFloat64("max-lon")
	
	lat := viper.GetFloat64("lat")
	lon := viper.GetFloat64("lon")
	width := viper.GetInt("width")
	height := viper.GetInt("height")

	// Check for centered mode
	if lat != 0 || lon != 0 || width != 0 || height != 0 {
		if lat == 0 || lon == 0 || width == 0 || height == 0 {
			return fmt.Errorf("centered mode requires all of: --lat, --lon, --width, --height")
		}
		return runCenteredMode(zoom, urls, lat, lon, width, height, format)
	}

	// Check for bounding box mode
	if bbox != "" {
		return runBboxStringMode(bbox, zoom, urls, format)
	}
	
	if minLat != 0 || maxLat != 0 || minLon != 0 || maxLon != 0 {
		if minLat == 0 || maxLat == 0 || minLon == 0 || maxLon == 0 {
			return fmt.Errorf("bounding box mode requires all of: --min-lat, --min-lon, --max-lat, --max-lon")
		}
		return runBboxMode(minLat, minLon, maxLat, maxLon, zoom, urls, format)
	}

	return fmt.Errorf("either specify bounding box coordinates (--min-lat, --min-lon, --max-lat, --max-lon or --bbox) or centered coordinates (--lat, --lon, --width, --height)")
}

func runBboxMode(minLat, minLon, maxLat, maxLon float64, zoom int, urls []string, format int) error {
	// Create stitch options
	opts := &tile.StitchOptions{
		Output:         viper.GetString("output"),
		TileSize:       viper.GetInt("tilesize"),
		Centered:       false,
		Format:         format,
		WriteWorldFile: viper.GetBool("worldfile"),
		UserAgent:      viper.GetString("user-agent"),
	}

	// Create stitcher
	stitcher := stitch.NewStitcher(opts)

	bbox := &tile.BoundingBox{
		MinLat: minLat,
		MinLon: minLon,
		MaxLat: maxLat,
		MaxLon: maxLon,
	}

	return stitcher.StitchBoundingBox(bbox, zoom, urls)
}

func runBboxStringMode(bboxStr string, zoom int, urls []string, format int) error {
	// Parse bbox string: "min-lat,min-lon,max-lat,max-lon"
	parts := strings.Split(bboxStr, ",")
	if len(parts) != 4 {
		return fmt.Errorf("bbox must be in format 'min-lat,min-lon,max-lat,max-lon'")
	}

	minLat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return fmt.Errorf("invalid min-lat in bbox: %v", err)
	}

	minLon, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return fmt.Errorf("invalid min-lon in bbox: %v", err)
	}

	maxLat, err := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
	if err != nil {
		return fmt.Errorf("invalid max-lat in bbox: %v", err)
	}

	maxLon, err := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
	if err != nil {
		return fmt.Errorf("invalid max-lon in bbox: %v", err)
	}

	return runBboxMode(minLat, minLon, maxLat, maxLon, zoom, urls, format)
}

func runCenteredMode(zoom int, urls []string, lat, lon float64, width, height int, format int) error {
	// Create stitch options
	opts := &tile.StitchOptions{
		Output:         viper.GetString("output"),
		TileSize:       viper.GetInt("tilesize"),
		Centered:       true,
		Format:         format,
		WriteWorldFile: viper.GetBool("worldfile"),
		UserAgent:      viper.GetString("user-agent"),
	}

	// Create stitcher
	stitcher := stitch.NewStitcher(opts)

	req := &tile.CenteredRequest{
		Lat:    lat,
		Lon:    lon,
		Width:  width,
		Height: height,
	}

	return stitcher.StitchCentered(req, zoom, urls)
}
