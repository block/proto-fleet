package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/mux"
	"github.com/rs/cors"
	"github.com/squareup/proto-fleet/minefield/internal/api"
	"github.com/squareup/proto-fleet/minefield/internal/errors"
	"github.com/squareup/proto-fleet/minefield/internal/proxy"
)

func main() {
	var (
		proxyAddr   = flag.String("proxy", ":7070", "Address for the proxy server")
		controlAddr = flag.String("control", ":7071", "Address for the control API")
		targetURL   = flag.String("target", "", "Target miner URL (or use PROXY_URL env)")
		verbose     = flag.Bool("verbose", false, "Enable verbose logging")
	)
	flag.Parse()

	// Get target URL from flag or environment
	target := *targetURL
	if target == "" {
		target = os.Getenv("PROXY_URL")
	}
	if target == "" {
		log.Fatal("Target URL required: use -target flag or set PROXY_URL environment variable")
	}

	// Parse target URL
	targetU, err := url.Parse(target)
	if err != nil {
		log.Fatalf("Invalid target URL %s: %v", target, err)
	}

	log.Printf("Starting Minefield proxy server")
	log.Printf("Proxy server: %s", *proxyAddr)
	log.Printf("Control API: %s", *controlAddr)
	log.Printf("Target miner: %s", targetU)

	// Create error store
	errorStore := errors.NewStore()

	// Create proxy handler
	proxyHandler, err := proxy.NewHandler(targetU, errorStore, *verbose)
	if err != nil {
		log.Fatalf("Failed to create proxy handler: %v", err)
	}

	// Start proxy server in goroutine
	go func() {
		log.Printf("Starting proxy server on %s", *proxyAddr)
		if err := http.ListenAndServe(*proxyAddr, proxyHandler); err != nil {
			log.Fatalf("Proxy server failed: %v", err)
		}
	}()

	// Create control API router
	router := mux.NewRouter()

	// API routes
	apiRouter := router.PathPrefix("/api").Subrouter()
	apiHandler := api.NewHandler(errorStore)
	apiHandler.RegisterRoutes(apiRouter)

	// Serve web UI from /web/dist if it exists
	webDir := "./web/dist"
	if _, err := os.Stat(webDir); err == nil {
		// Serve static files
		router.PathPrefix("/").Handler(http.FileServer(http.Dir(webDir)))
		log.Printf("Serving web UI from %s", webDir)
	} else {
		// Fallback message if web UI not built
		router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `
				<html>
					<body style="font-family: system-ui; padding: 2rem;">
						<h1>Minefield Control Panel</h1>
						<p>Web UI not built. To build and run the web interface:</p>
						<pre style="background: #f0f0f0; padding: 1rem;">
cd minefield
just web-build
just run</pre>
						<p>Or use the CLI tool:</p>
						<pre style="background: #f0f0f0; padding: 1rem;">
./bin/minefield-cli --help</pre>
						<p>API endpoints are available at <a href="/api/status">/api/status</a></p>
					</body>
				</html>
			`)
		})
	}

	// Setup CORS for control API
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	})

	// Start control API server in goroutine
	go func() {
		log.Printf("Starting control API on %s", *controlAddr)
		if err := http.ListenAndServe(*controlAddr, c.Handler(router)); err != nil {
			log.Fatalf("Control API server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down...")
}