package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

func (s *Server) spaHandler() http.Handler {
	files := http.FileServerFS(s.static)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			respondError(w, r, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		name := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if name == "." || name == "/" {
			s.serveIndex(w, r)
			return
		}

		info, err := fs.Stat(s.static, name)
		if err != nil || info.IsDir() {
			s.serveIndex(w, r)
			return
		}

		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	index, err := fs.ReadFile(s.static, "index.html")
	if err != nil {
		respondError(w, r, http.StatusNotFound, "frontend is not built into this binary")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(index)
}
