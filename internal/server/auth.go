package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

// TokenAuth adds opt-in authentication for all shared-server endpoints.
func TokenAuth(next http.Handler) http.Handler {
	token := os.Getenv("ATLAS_SERVER_TOKEN")
	if token == "" {
		return next
	}
	expected := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		provided := sha256.Sum256([]byte(strings.TrimPrefix(header, "Bearer ")))
		if !strings.HasPrefix(header, "Bearer ") || subtle.ConstantTimeCompare(expected[:], provided[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
