package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/squareup/proto-fleet/minefield/internal/errors"
)

// Handler is the main proxy handler that intercepts requests
type Handler struct {
	proxy      *httputil.ReverseProxy
	target     *url.URL
	errorStore *errors.Store
	verbose    bool
}

// NewHandler creates a new proxy handler
func NewHandler(target *url.URL, errorStore *errors.Store, verbose bool) (*Handler, error) {
	proxy := httputil.NewSingleHostReverseProxy(target)

	// Customize the proxy director to preserve headers
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		// Preserve original host header if needed
		if _, ok := req.Header["User-Agent"]; !ok {
			req.Header.Set("User-Agent", "Minefield-Proxy/1.0")
		}
	}

	handler := &Handler{
		proxy:      proxy,
		target:     target,
		errorStore: errorStore,
		verbose:    verbose,
	}

	// Set up response modification for error injection
	proxy.ModifyResponse = handler.modifyResponse

	return handler, nil
}

// ServeHTTP implements http.Handler
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.verbose {
		log.Printf("[PROXY] %s %s", r.Method, r.URL.Path)
	}

	// Check if this is the errors endpoint
	if r.URL.Path == "/api/v1/errors" && r.Method == "GET" {
		// Intercept and modify the response
		h.proxyWithInterception(w, r)
		return
	}

	// Pass through all other requests unchanged
	h.proxy.ServeHTTP(w, r)
}

// proxyWithInterception handles requests that need response modification
func (h *Handler) proxyWithInterception(w http.ResponseWriter, r *http.Request) {
	// Create a custom response writer to capture the response
	recorder := &responseRecorder{
		ResponseWriter: w,
		body:          new(bytes.Buffer),
		headers:       make(http.Header),
	}

	// Proxy the request
	h.proxy.ServeHTTP(recorder, r)

	// Get the original response body
	originalBody := recorder.body.Bytes()

	// Parse the original errors response
	var originalErrors []interface{}
	if len(originalBody) > 0 {
		if err := json.Unmarshal(originalBody, &originalErrors); err != nil {
			// If we can't parse it, just return the original
			if h.verbose {
				log.Printf("[PROXY] Failed to parse errors response: %v", err)
			}
			w.Write(originalBody)
			return
		}
	}

	// Get injected errors from our store
	injectedErrors := h.errorStore.GetActiveErrors()

	// Combine original and injected errors
	combinedErrors := make([]interface{}, 0, len(originalErrors)+len(injectedErrors))

	// Add injected errors first (so they appear at the top)
	for _, err := range injectedErrors {
		combinedErrors = append(combinedErrors, err.ToAPIFormat())
	}

	// Add original errors
	combinedErrors = append(combinedErrors, originalErrors...)

	// Marshal the combined response
	responseBody, err := json.Marshal(combinedErrors)
	if err != nil {
		log.Printf("[PROXY] Failed to marshal combined errors: %v", err)
		w.Write(originalBody)
		return
	}

	// Write the modified response
	for k, v := range recorder.headers {
		w.Header()[k] = v
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(responseBody)))
	w.WriteHeader(recorder.statusCode)
	w.Write(responseBody)

	if h.verbose {
		log.Printf("[PROXY] Injected %d errors into response (total: %d)",
			len(injectedErrors), len(combinedErrors))
	}
}

// modifyResponse is called for all responses when using ReverseProxy
func (h *Handler) modifyResponse(resp *http.Response) error {
	if h.verbose && strings.HasPrefix(resp.Request.URL.Path, "/api/") {
		log.Printf("[PROXY] Response: %s %s -> %d",
			resp.Request.Method, resp.Request.URL.Path, resp.StatusCode)
	}
	return nil
}

// responseRecorder captures the response for modification
type responseRecorder struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
	headers    http.Header
	written    bool
}

func (r *responseRecorder) WriteHeader(code int) {
	if !r.written {
		r.statusCode = code
		// Copy headers
		for k, v := range r.ResponseWriter.Header() {
			r.headers[k] = v
		}
		r.written = true
	}
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.written {
		r.WriteHeader(http.StatusOK)
	}
	return r.body.Write(b)
}

// Ensure we capture the body
func (r *responseRecorder) ReadFrom(src io.Reader) (int64, error) {
	if !r.written {
		r.WriteHeader(http.StatusOK)
	}
	return io.Copy(r.body, src)
}