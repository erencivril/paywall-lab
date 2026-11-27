package api

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// logSafe removes line breaks from a client-controlled value before it is
// logged, so a request cannot forge extra log lines.
func logSafe(s string) string {
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) > 256 {
		s = s[:256]
	}
	return s
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// requestLog gives every request an id (also returned as X-Request-Id) and
// logs one line when it finishes.
func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A caller may supply an id for tracing, but only a plain one. Anything
		// else (control characters, long strings) is replaced, so the value is
		// safe to echo in a header and write to the log.
		id := r.Header.Get("X-Request-Id")
		if !requestIDPattern.MatchString(id) {
			var b [8]byte
			_, _ = rand.Read(b[:])
			id = hex.EncodeToString(b[:])
		}
		w.Header().Set("X-Request-Id", id)

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)

		slog.Info("request",
			"request_id", logSafe(id),
			"method", logSafe(r.Method),
			"path", logSafe(r.URL.Path),
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				slog.Error("panic in handler", "panic", v, "path", logSafe(r.URL.Path))
				writeError(w, http.StatusInternalServerError, "internal_error", "something went wrong")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// securityHeaders sets response headers that browsers use to limit what the
// paywall page can do. The page loads only its own script and stylesheet, so
// the policy allows same-origin resources and nothing else.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}
