package daemon

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
)

// generateToken creates a cryptographically random 32-byte token encoded as
// base64url (no padding). Used to auto-generate the TCP bearer token when no
// explicit AuthToken is provided.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// corsMiddleware wraps next and enforces the CORS origin allowlist on TCP
// requests. Only origins in d.config.CORSOrigins are permitted. If the
// request carries no Origin header, CORS headers are not emitted but the
// request is passed through (non-browser clients, curl, internal callers).
// Preflight OPTIONS requests are short-circuited with 204 No Content before
// the auth middleware runs, so browser preflight probes work without a token.
//
// Chain order on TCP: corsMiddleware → authMiddleware → handler
// CORS is outermost so OPTIONS short-circuits before auth is checked.
func (d *Daemon) corsMiddleware(next http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(d.config.CORSOrigins))
	for _, o := range d.config.CORSOrigins {
		allowed[o] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if _, ok := allowed[origin]; !ok {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "origin not allowed"})
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Last-Event-ID")
			// Never set Access-Control-Allow-Credentials (token is in header, not cookie).
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authMiddleware wraps next and enforces bearer-token authentication on TCP
// requests. GET /health is exempt so liveness probes work without credentials.
// Unix-socket requests are served via d.server directly (never through this
// middleware), so they bypass auth entirely.
func (d *Daemon) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /health is exempt — liveness probes must not require a token.
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing bearer token"})
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(d.authToken)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

