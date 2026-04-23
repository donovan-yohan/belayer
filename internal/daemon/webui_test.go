package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWebUI_ServesIndex verifies that GET /ui/ returns 200 and HTML containing
// "Belayer".
func TestWebUI_ServesIndex(t *testing.T) {
	d := testDaemon(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ui/", nil)
	d.server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Belayer") {
		t.Fatalf("expected body to contain 'Belayer', got %q", rr.Body.String())
	}
}

// TestWebUI_ServesCSS verifies that GET /ui/style.css returns 200 with
// text/css Content-Type.
func TestWebUI_ServesCSS(t *testing.T) {
	d := testDaemon(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ui/style.css", nil)
	d.server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/css" {
		t.Fatalf("expected Content-Type text/css, got %q", ct)
	}
}

// TestWebUI_ServesJS verifies that GET /ui/app.js returns 200 with
// application/javascript Content-Type.
func TestWebUI_ServesJS(t *testing.T) {
	d := testDaemon(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ui/app.js", nil)
	d.server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/javascript" {
		t.Fatalf("expected Content-Type application/javascript, got %q", ct)
	}
}
