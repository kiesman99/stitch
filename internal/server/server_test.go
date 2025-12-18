package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/kiesman99/stitch/internal/api"
)

// Test server setup
func setupTestServer() *httptest.Server {
	r := chi.NewRouter()

	// Add middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Timeout(30 * time.Second))

	// CORS middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	})

	// Create server implementation
	apiServer := NewServer("2.0.0-test")

	// Mount API routes at /api/v1
	r.Route("/api/v1", func(r chi.Router) {
		handler := api.HandlerWithOptions(apiServer, api.ChiServerOptions{
			BaseRouter: r,
		})
		r.Mount("/", handler)
	})

	return httptest.NewServer(r)
}

func TestHealthEndpoint(t *testing.T) {
	server := setupTestServer()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/health")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	// Parse response
	var healthResp api.HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Validate response
	if healthResp.Status != api.Healthy {
		t.Errorf("Expected status 'healthy', got %s", healthResp.Status)
	}

	if healthResp.Version == nil || *healthResp.Version != "2.0.0-test" {
		t.Errorf("Expected version '2.0.0-test', got %v", healthResp.Version)
	}

	if healthResp.Uptime == nil || *healthResp.Uptime < 0 {
		t.Errorf("Expected valid uptime, got %v", healthResp.Uptime)
	}

	// Check timestamp is recent
	if time.Since(healthResp.Timestamp) > time.Minute {
		t.Errorf("Timestamp seems too old: %v", healthResp.Timestamp)
	}
}

func TestStitchEndpoint_BoundingBox_Success(t *testing.T) {
	server := setupTestServer()
	defer server.Close()

	// Create a valid bounding box request
	request := api.StitchRequest{
		Mode: api.Bbox,
		Bbox: &api.BoundingBox{
			MinLat: 37.7,
			MinLon: -122.5,
			MaxLat: 37.8,
			MaxLon: -122.4,
		},
		Zoom: 8, // Low zoom to minimize tiles
		TileSource: api.TileSource{
			Url:  "https://tile.opentopomap.org/{z}/{x}/{y}.png",
			Name: stringPtr("OpenTopoMap"),
		},
		Output: &api.OutputOptions{
			Format:   (*api.OutputOptionsFormat)(&[]api.OutputOptionsFormat{api.Png}[0]),
			TileSize: (*api.OutputOptionsTileSize)(&[]api.OutputOptionsTileSize{api.N256}[0]),
		},
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	resp, err := http.Post(
		server.URL+"/api/v1/stitch",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if contentType != "image/png" {
		t.Errorf("Expected Content-Type image/png, got %s", contentType)
	}

	// Check that we got image data
	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if len(imageData) == 0 {
		t.Error("Expected image data, got empty response")
	}

	// Check PNG signature
	if len(imageData) < 8 || !bytes.Equal(imageData[:8], []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) {
		t.Error("Response does not appear to be a valid PNG file")
	}

	// Check request ID header
	requestID := resp.Header.Get("X-Request-ID")
	if requestID == "" {
		t.Error("Expected X-Request-ID header")
	}
}

func TestStitchEndpoint_Centered_Success(t *testing.T) {
	server := setupTestServer()
	defer server.Close()

	// Create a valid centered request
	request := api.StitchRequest{
		Mode: api.Centered,
		Center: &api.CenterPoint{
			Lat:    37.7749,
			Lon:    -122.4194,
			Width:  256,
			Height: 256,
		},
		Zoom: 10,
		TileSource: api.TileSource{
			Url: "https://tile.opentopomap.org/{z}/{x}/{y}.png",
		},
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	resp, err := http.Post(
		server.URL+"/api/v1/stitch",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if contentType != "image/png" {
		t.Errorf("Expected Content-Type image/png, got %s", contentType)
	}

	// Check that we got image data
	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if len(imageData) == 0 {
		t.Error("Expected image data, got empty response")
	}
}

func TestStitchEndpoint_ValidationErrors(t *testing.T) {
	server := setupTestServer()
	defer server.Close()

	testCases := []struct {
		name           string
		request        interface{}
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "Invalid JSON",
			request:        `{"invalid": json}`,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "INVALID_JSON",
		},
		{
			name: "Missing bbox in bbox mode",
			request: api.StitchRequest{
				Mode: api.Bbox,
				Zoom: 10,
				TileSource: api.TileSource{
					Url: "https://example.com/{z}/{x}/{y}.png",
				},
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "VALIDATION_ERROR",
		},
		{
			name: "Missing center in centered mode",
			request: api.StitchRequest{
				Mode: api.Centered,
				Zoom: 10,
				TileSource: api.TileSource{
					Url: "https://example.com/{z}/{x}/{y}.png",
				},
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "VALIDATION_ERROR",
		},
		{
			name: "Invalid zoom level",
			request: api.StitchRequest{
				Mode: api.Bbox,
				Bbox: &api.BoundingBox{
					MinLat: 37.7,
					MinLon: -122.5,
					MaxLat: 37.8,
					MaxLon: -122.4,
				},
				Zoom: 25, // Too high
				TileSource: api.TileSource{
					Url: "https://example.com/{z}/{x}/{y}.png",
				},
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "VALIDATION_ERROR",
		},
		{
			name: "Invalid tile URL template",
			request: api.StitchRequest{
				Mode: api.Bbox,
				Bbox: &api.BoundingBox{
					MinLat: 37.7,
					MinLon: -122.5,
					MaxLat: 37.8,
					MaxLon: -122.4,
				},
				Zoom: 10,
				TileSource: api.TileSource{
					Url: "https://example.com/tile.png", // Missing placeholders
				},
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "VALIDATION_ERROR",
		},
		{
			name: "Invalid bounding box coordinates",
			request: api.StitchRequest{
				Mode: api.Bbox,
				Bbox: &api.BoundingBox{
					MinLat: 37.8, // Min > Max
					MinLon: -122.5,
					MaxLat: 37.7,
					MaxLon: -122.4,
				},
				Zoom: 10,
				TileSource: api.TileSource{
					Url: "https://example.com/{z}/{x}/{y}.png",
				},
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "VALIDATION_ERROR",
		},
		{
			name: "Invalid center dimensions",
			request: api.StitchRequest{
				Mode: api.Centered,
				Center: &api.CenterPoint{
					Lat:    37.7749,
					Lon:    -122.4194,
					Width:  0, // Invalid
					Height: 256,
				},
				Zoom: 10,
				TileSource: api.TileSource{
					Url: "https://example.com/{z}/{x}/{y}.png",
				},
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "VALIDATION_ERROR",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var body io.Reader

			if str, ok := tc.request.(string); ok {
				body = strings.NewReader(str)
			} else {
				jsonData, err := json.Marshal(tc.request)
				if err != nil {
					t.Fatalf("Failed to marshal request: %v", err)
				}
				body = bytes.NewBuffer(jsonData)
			}

			resp, err := http.Post(
				server.URL+"/api/v1/stitch",
				"application/json",
				body,
			)
			if err != nil {
				t.Fatalf("Failed to make request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.expectedStatus {
				responseBody, _ := io.ReadAll(resp.Body)
				t.Errorf("Expected status %d, got %d. Body: %s", tc.expectedStatus, resp.StatusCode, string(responseBody))
			}

			// Parse error response
			var errorResp map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
				t.Fatalf("Failed to decode error response: %v", err)
			}

			if errorCode, ok := errorResp["error"].(string); !ok || errorCode != tc.expectedError {
				t.Errorf("Expected error code %s, got %v", tc.expectedError, errorResp["error"])
			}
		})
	}
}

func TestStitchEndpoint_TileServerError(t *testing.T) {
	server := setupTestServer()
	defer server.Close()

	// Create request with non-existent tile server
	request := api.StitchRequest{
		Mode: api.Bbox,
		Bbox: &api.BoundingBox{
			MinLat: 37.7,
			MinLon: -122.5,
			MaxLat: 37.8,
			MaxLon: -122.4,
		},
		Zoom: 10,
		TileSource: api.TileSource{
			Url: "https://nonexistent.tile.server.invalid/{z}/{x}/{y}.png",
		},
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	resp, err := http.Post(
		server.URL+"/api/v1/stitch",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Should get a tile server error (502)
	if resp.StatusCode != http.StatusBadGateway {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected status 502, got %d. Body: %s", resp.StatusCode, string(body))
	}

	// Parse error response
	var errorResp api.TileErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
		t.Fatalf("Failed to decode error response: %v", err)
	}

	if errorResp.Error != "TILE_SERVER_ERROR" {
		t.Errorf("Expected error code TILE_SERVER_ERROR, got %s", errorResp.Error)
	}

	if errorResp.TotalTiles == 0 {
		t.Error("Expected total_tiles > 0")
	}

	if len(errorResp.FailedTiles) == 0 {
		t.Error("Expected failed_tiles to be populated")
	}
}

func TestStitchEndpoint_Timeout(t *testing.T) {
	// This test would require a mock server that delays responses
	// For now, we'll skip it as it's complex to set up
	t.Skip("Timeout test requires mock server setup")
}

func TestCORSHeaders(t *testing.T) {
	server := setupTestServer()
	defer server.Close()

	// Test OPTIONS request
	req, err := http.NewRequest("OPTIONS", server.URL+"/api/v1/stitch", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Check CORS headers
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Error("Expected Access-Control-Allow-Origin: *")
	}

	if !strings.Contains(resp.Header.Get("Access-Control-Allow-Methods"), "POST") {
		t.Error("Expected Access-Control-Allow-Methods to include POST")
	}

	if !strings.Contains(resp.Header.Get("Access-Control-Allow-Headers"), "Content-Type") {
		t.Error("Expected Access-Control-Allow-Headers to include Content-Type")
	}
}

func TestStitchEndpoint_WithCustomHeaders(t *testing.T) {
	server := setupTestServer()
	defer server.Close()

	// Create request with custom headers
	headers := map[string]string{
		"User-Agent": "CustomAgent/1.0",
		"Referer":    "https://example.com",
	}

	request := api.StitchRequest{
		Mode: api.Bbox,
		Bbox: &api.BoundingBox{
			MinLat: 37.7,
			MinLon: -122.5,
			MaxLat: 37.8,
			MaxLon: -122.4,
		},
		Zoom: 8,
		TileSource: api.TileSource{
			Url:     "https://tile.opentopomap.org/{z}/{x}/{y}.png",
			Headers: &headers,
		},
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	resp, err := http.Post(
		server.URL+"/api/v1/stitch",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Should succeed (headers are passed through to tile requests)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
	}
}

// Helper function
func stringPtr(s string) *string {
	return &s
}
