package main

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
)

// newHTTPHandler keeps the API mux separate so the static fallback cannot mask
// its JSON 404/405 responses or change the existing /health method behavior.
func newHTTPHandler(state *ContainerState, webRoot string) (http.Handler, func(), error) {
	mux := http.NewServeMux()
	NewRESTApiHandler(state).RegisterRoutes(mux)
	registerConnectRoutes(mux, state)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	api := jsonErrorMiddleware(mux)
	if webRoot == "" {
		return api, func() {}, nil
	}

	web, err := newWebHandler(webRoot)
	if err != nil {
		return nil, nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if strings.HasPrefix(name, "mdk.v2.") {
			api.ServeHTTP(w, r)
			return
		}
		for _, prefix := range []string{"api", "health", "metrics", "debug"} {
			if name == prefix || strings.HasPrefix(name, prefix+"/") {
				api.ServeHTTP(w, r)
				return
			}
		}
		web.ServeHTTP(w, r)
	}), func() { _ = web.root.Close() }, nil
}

// webHandler previews a production build, not a firmware package: unlike
// MDK v2 host artifact verification, it only requires a readable built index.html.
// os.Root confines file access, including symlinks, to the selected directory.
type webHandler struct {
	root *os.Root
}

func newWebHandler(webRoot string) (*webHandler, error) {
	root, err := os.OpenRoot(webRoot)
	if err != nil {
		return nil, fmt.Errorf("invalid --web-root %q: %w", webRoot, err)
	}
	// Open nonblocking so a FIFO replacement cannot block before validation.
	index, err := root.OpenFile("index.html", os.O_RDONLY|webNonblock, 0)
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("invalid --web-root %q: open built index.html: %w", webRoot, err)
	}
	info, err := index.Stat()
	_ = index.Close()
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		_ = root.Close()
		return nil, fmt.Errorf("invalid --web-root %q: index.html must be a readable, non-empty regular file", webRoot)
	}
	return &webHandler{root: root}, nil
}

func (h *webHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Match the MDK v2 host's static-path and browser-route rules. Reject dot segments
	// before cleaning the path so traversal never turns into an allowed route.
	for _, segment := range strings.Split(r.URL.Path, "/") {
		if strings.HasPrefix(segment, ".") || strings.ContainsAny(segment, "\\\x00") {
			http.NotFound(w, r)
			return
		}
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	staticPath := name == "assets" || name == "fonts" ||
		strings.HasPrefix(name, "assets/") || strings.HasPrefix(name, "fonts/")
	publicFile := name == "index.html" || name == "config.js" ||
		name == "version.txt" || name == "favicon.png"
	if publicFile || staticPath {
		h.serveFile(w, r, name)
		return
	}
	if path.Ext(name) == "" && isProtoOSBrowserRoute(name) {
		h.serveFile(w, r, "index.html")
		return
	}
	http.NotFound(w, r)
}

// Match MDK v2 device-served ProtoOS route prefixes. Arbitrary misses
// must stay 404, especially stale asset URLs and unsupported API namespaces.
func isProtoOSBrowserRoute(name string) bool {
	prefix, _, _ := strings.Cut(name, "/")
	switch prefix {
	case "hashrate", "efficiency", "power-usage", "temperature", "logs", "pools",
		"systems", "tanks", "modules", "onboarding", "settings":
		return true
	default:
		return false
	}
}

func (h *webHandler) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	// Validate the opened descriptor, not a path that can change before Open.
	file, err := h.root.OpenFile(name, os.O_RDONLY|webNonblock, 0)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	// This is a local smoke-test fixture; never retain stale builds in a browser.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, name, info.ModTime(), file)
}
