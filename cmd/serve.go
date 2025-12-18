package cmd

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/kiesman99/stitch/internal/api"
	"github.com/kiesman99/stitch/internal/server"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start HTTP server for tile stitching API",
	Long: `Start an HTTP server that provides a REST API for tile stitching.

The server provides endpoints for both bounding box and centered tile stitching.

Examples:
  # Start server on default port 8080
  stitch serve

  # Start server on custom port
  stitch serve --port 3000

  # Start server with custom bind address
  stitch serve --bind 0.0.0.0 --port 8080`,
	RunE: runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)

	// Server configuration
	serveCmd.Flags().StringP("bind", "b", "localhost", "bind address")
	serveCmd.Flags().IntP("port", "p", 8080, "port to listen on")
	serveCmd.Flags().Duration("timeout", 30*time.Second, "request timeout")

	// Bind flags to viper
	viper.BindPFlag("server.bind", serveCmd.Flags().Lookup("bind"))
	viper.BindPFlag("server.port", serveCmd.Flags().Lookup("port"))
	viper.BindPFlag("server.timeout", serveCmd.Flags().Lookup("timeout"))
}

func runServe(cmd *cobra.Command, args []string) error {
	bind := viper.GetString("server.bind")
	port := viper.GetInt("server.port")
	timeout := viper.GetDuration("server.timeout")

	addr := fmt.Sprintf("%s:%d", bind, port)

	// Create Chi router
	r := chi.NewRouter()

	// Add middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Timeout(timeout))

	// CORS middleware for API access
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
	apiServer := server.NewServer("2.0.0")

	// Mount API routes at /api/v1
	r.Route("/api/v1", func(r chi.Router) {
		// Use the generated Chi handler
		handler := api.HandlerWithOptions(apiServer, api.ChiServerOptions{
			BaseRouter: r,
		})
		r.Mount("/", handler)
	})

	// Legacy health endpoint (without /api/v1 prefix for backward compatibility)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		// Redirect to the API health endpoint
		http.Redirect(w, r, "/api/v1/health", http.StatusMovedPermanently)
	})

	httpServer := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		fmt.Fprintf(cmd.ErrOrStderr(), "\nShutting down server...\n")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}()

	fmt.Fprintf(cmd.ErrOrStderr(), "Starting stitch server on %s\n", addr)
	fmt.Fprintf(cmd.ErrOrStderr(), "API documentation: http://%s/\n", addr)
	fmt.Fprintf(cmd.ErrOrStderr(), "Health check: http://%s/api/v1/health\n", addr)
	fmt.Fprintf(cmd.ErrOrStderr(), "Stitch endpoint: http://%s/api/v1/stitch\n", addr)

	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("server error: %v", err)
	}

	return nil
}
