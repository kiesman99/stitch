package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kiesman99/stitch/internal/api"
	"github.com/kiesman99/stitch/internal/stitcher"
)

// Server implements the ServerInterface from the generated API
type Server struct {
	startTime time.Time
	version   string
}

// NewServer creates a new server instance
func NewServer(version string) *Server {
	return &Server{
		startTime: time.Now(),
		version:   version,
	}
}

// GetHealth implements the health check endpoint
func (s *Server) GetHealth(w http.ResponseWriter, r *http.Request) {
	uptime := int(time.Since(s.startTime).Seconds())

	response := api.HealthResponse{
		Status:    api.Healthy,
		Timestamp: time.Now(),
		Uptime:    &uptime,
		Version:   &s.version,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding health response: %v", err)
	}
}

// CreateStitchedImage implements the main stitching endpoint
func (s *Server) CreateStitchedImage(w http.ResponseWriter, r *http.Request) {
	// Generate request ID for tracking
	requestID := generateRequestID()

	// Parse request body
	var req api.StitchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "INVALID_JSON",
			"Invalid JSON in request body", &requestID, nil)
		return
	}

	// Validate request
	if err := s.validateStitchRequest(&req); err != nil {
		s.writeValidationErrorResponse(w, err.Error(), &requestID)
		return
	}

	// Convert API request to stitcher options
	opts, err := s.convertToStitcherOptions(&req)
	if err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "INVALID_REQUEST",
			err.Error(), &requestID, nil)
		return
	}

	// Create stitcher instance
	st := stitcher.New()

	// Perform stitching
	result, err := st.Stitch(r.Context(), opts)
	if err != nil {
		s.handleStitchingError(w, err, &requestID)
		return
	}

	// Set appropriate content type based on output format
	format := api.Png // default
	if req.Output != nil && req.Output.Format != nil {
		format = *req.Output.Format
	}

	switch format {
	case api.Png:
		w.Header().Set("Content-Type", "image/png")
	case api.Geotiff:
		w.Header().Set("Content-Type", "image/tiff")
	}

	// Set additional headers
	w.Header().Set("X-Request-ID", requestID)
	w.Header().Set("Content-Length", strconv.Itoa(len(result.ImageData)))

	// Write image data
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(result.ImageData); err != nil {
		log.Printf("Error writing response: %v", err)
	}
}

// validateStitchRequest validates the incoming stitch request
func (s *Server) validateStitchRequest(req *api.StitchRequest) error {
	// Validate mode and corresponding parameters
	switch req.Mode {
	case api.Bbox:
		if req.Bbox == nil {
			return fmt.Errorf("bbox is required when mode is 'bbox'")
		}
		if req.Center != nil {
			return fmt.Errorf("center should not be provided when mode is 'bbox'")
		}
		// Validate bbox bounds
		if req.Bbox.MinLat >= req.Bbox.MaxLat {
			return fmt.Errorf("min_lat must be less than max_lat")
		}
		if req.Bbox.MinLon >= req.Bbox.MaxLon {
			return fmt.Errorf("min_lon must be less than max_lon")
		}
	case api.Centered:
		if req.Center == nil {
			return fmt.Errorf("center is required when mode is 'centered'")
		}
		if req.Bbox != nil {
			return fmt.Errorf("bbox should not be provided when mode is 'centered'")
		}
		// Validate center dimensions
		if req.Center.Width <= 0 || req.Center.Height <= 0 {
			return fmt.Errorf("width and height must be positive")
		}
	default:
		return fmt.Errorf("invalid mode: %s", req.Mode)
	}

	// Validate zoom level
	if req.Zoom < 0 || req.Zoom > 20 {
		return fmt.Errorf("zoom must be between 0 and 20")
	}

	// Validate tile source URL
	if req.TileSource.Url == "" {
		return fmt.Errorf("tile_source.url is required")
	}
	if !strings.Contains(req.TileSource.Url, "{z}") ||
		!strings.Contains(req.TileSource.Url, "{x}") ||
		!strings.Contains(req.TileSource.Url, "{y}") {
		return fmt.Errorf("tile_source.url must contain {z}, {x}, and {y} placeholders")
	}

	return nil
}

// convertToStitcherOptions converts API request to internal stitcher options
func (s *Server) convertToStitcherOptions(req *api.StitchRequest) (*stitcher.Options, error) {
	opts := &stitcher.Options{
		Zoom:     req.Zoom,
		TileURLs: []string{req.TileSource.Url},
		TileSize: 256, // default
	}

	// Set tile size if specified
	if req.Output != nil && req.Output.TileSize != nil {
		opts.TileSize = int(*req.Output.TileSize)
	}

	// Set output format
	if req.Output != nil && req.Output.Format != nil {
		switch *req.Output.Format {
		case api.Png:
			opts.OutputFormat = stitcher.FormatPNG
		case api.Geotiff:
			opts.OutputFormat = stitcher.FormatGeoTIFF
		}
	} else {
		opts.OutputFormat = stitcher.FormatPNG
	}

	// Set world file generation
	if req.Output != nil && req.Output.GenerateWorldfile != nil {
		opts.GenerateWorldFile = *req.Output.GenerateWorldfile
	}

	// Set headers if provided
	if req.TileSource.Headers != nil {
		opts.Headers = *req.TileSource.Headers
	}

	// Set coordinates based on mode
	switch req.Mode {
	case api.Bbox:
		opts.Mode = stitcher.ModeBBox
		opts.MinLat = float64(req.Bbox.MinLat)
		opts.MinLon = float64(req.Bbox.MinLon)
		opts.MaxLat = float64(req.Bbox.MaxLat)
		opts.MaxLon = float64(req.Bbox.MaxLon)
	case api.Centered:
		opts.Mode = stitcher.ModeCentered
		opts.CenterLat = float64(req.Center.Lat)
		opts.CenterLon = float64(req.Center.Lon)
		opts.Width = req.Center.Width
		opts.Height = req.Center.Height
	}

	return opts, nil
}

// handleStitchingError handles errors from the stitching process
func (s *Server) handleStitchingError(w http.ResponseWriter, err error, requestID *string) {
	// Check if it's a tile-related error
	if stitchErr, ok := err.(*stitcher.TileError); ok {
		// Convert to API tile error response
		failedTiles := make([]struct {
			Error      string `json:"error"`
			StatusCode *int   `json:"status_code,omitempty"`
			Url        string `json:"url"`
		}, len(stitchErr.FailedTiles))

		for i, ft := range stitchErr.FailedTiles {
			failedTiles[i] = struct {
				Error      string `json:"error"`
				StatusCode *int   `json:"status_code,omitempty"`
				Url        string `json:"url"`
			}{
				Error:      ft.Error,
				StatusCode: ft.StatusCode,
				Url:        ft.URL,
			}
		}

		response := api.TileErrorResponse{
			Error:           "TILE_SERVER_ERROR",
			Message:         stitchErr.Message,
			FailedTiles:     failedTiles,
			SuccessfulTiles: stitchErr.SuccessfulTiles,
			TotalTiles:      stitchErr.TotalTiles,
			RequestId:       requestID,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Check if it's a timeout error
	if err == context.DeadlineExceeded {
		s.writeErrorResponse(w, http.StatusGatewayTimeout, "TILE_SERVER_TIMEOUT",
			"Tile server requests timed out", requestID, map[string]interface{}{
				"timeout_seconds": 30,
			})
		return
	}

	// Generic internal server error
	s.writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR",
		"Internal server error", requestID, nil)
}

// writeErrorResponse writes a standard error response
func (s *Server) writeErrorResponse(w http.ResponseWriter, statusCode int, errorCode, message string, requestID *string, details map[string]interface{}) {
	response := api.ErrorResponse{
		Error:     errorCode,
		Message:   message,
		RequestId: requestID,
	}

	if details != nil {
		response.Details = &details
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}

// writeValidationErrorResponse writes a validation error response
func (s *Server) writeValidationErrorResponse(w http.ResponseWriter, message string, requestID *string) {
	response := api.ValidationErrorResponse{
		Error:     api.VALIDATIONERROR,
		Message:   message,
		RequestId: requestID,
		ValidationErrors: []struct {
			Code    *string `json:"code,omitempty"`
			Field   string  `json:"field"`
			Message string  `json:"message"`
		}{
			{
				Field:   "request",
				Message: message,
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(response)
}

// generateRequestID generates a unique request ID
func generateRequestID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}
