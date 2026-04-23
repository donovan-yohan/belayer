package daemon

import (
	"net/http"
	"strings"

	"github.com/donovan-yohan/belayer"
)

func (d *Daemon) handleWebUI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/ui")
	if path == "" || path == "/" {
		path = "/index.html"
	}

	content, err := belayer.WebUI.ReadFile("web" + path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	switch {
	case strings.HasSuffix(path, ".css"):
		w.Header().Set("Content-Type", "text/css")
	case strings.HasSuffix(path, ".js"):
		w.Header().Set("Content-Type", "application/javascript")
	default:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	w.Write(content)
}
