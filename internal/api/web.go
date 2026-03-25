package api

import (
	"embed"
	"log/slog"
	"net/http"
)

//go:embed templates/index.html
var templatesFS embed.FS

// indexHTML is the cached content of index.html
var indexHTML []byte

func init() {
	var err error
	indexHTML, err = templatesFS.ReadFile("templates/index.html")
	if err != nil {
		panic("failed to read embedded index.html: " + err.Error())
	}
}

// ServeIndex serves the web UI index page
func (r *Router) ServeIndex(w http.ResponseWriter, req *http.Request) {
	// Check if it's an API route
	if len(req.URL.Path) >= 4 && req.URL.Path[:4] == "/api" {
		http.NotFound(w, req)
		return
	}

	// Serve the embedded index.html
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(indexHTML); err != nil {
		slog.Error("Failed to write index.html", "error", err)
	}
}
