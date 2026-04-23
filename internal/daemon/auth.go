package daemon

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
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

// normalizeOrigin returns the canonical scheme://host[:port] form used by
// browsers in the Origin header. Trailing slashes, paths, queries, fragments,
// and mixed-case schemes/hosts on the configured allowlist entry would all
// cause a byte-for-byte map lookup to miss a legitimate browser request.
// Invalid inputs (unparseable, missing scheme, missing host) return "" so
// the caller can reject them at startup.
func normalizeOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
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
//
// Allowlist entries are normalized to lowercase scheme://host[:port] at
// startup so configs like "HTTPS://Example.com/" match the canonical form
// browsers send.
func (d *Daemon) corsMiddleware(next http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(d.config.CORSOrigins))
	for _, o := range d.config.CORSOrigins {
		if norm := normalizeOrigin(o); norm != "" {
			allowed[norm] = struct{}{}
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			// Browsers already send canonical form, but we normalize the
			// incoming value too so a misbehaving client cannot bypass the
			// allowlist via casing tricks.
			if _, ok := allowed[normalizeOrigin(origin)]; !ok {
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
		// /ui/ and /ui/* are exempt — static assets must load without a token.
		if strings.HasPrefix(r.URL.Path, "/ui/") {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth == "" && r.Method == http.MethodGet {
			auth = "Bearer " + r.URL.Query().Get("token")
		}
		if !strings.HasPrefix(auth, "Bearer ") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing bearer token"})
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		// subtle.ConstantTimeCompare short-circuits on length mismatch, which
		// leaks the length of the expected token over timing. Hash both
		// inputs to a fixed-width digest first so the compare always runs
		// over 32 bytes regardless of the presented token's length.
		wantDigest := sha256.Sum256([]byte(d.authToken))
		gotDigest := sha256.Sum256([]byte(token))
		if subtle.ConstantTimeCompare(wantDigest[:], gotDigest[:]) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

