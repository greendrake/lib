package apiserver

import (
	"net/http"
	"slices"
	"strings"
)

// baseAllowHeaders are the request headers every API wrapped in CORS accepts.
var baseAllowHeaders = []string{
	"Content-Type", "Content-Length", "Accept-Encoding", "X-CSRF-Token", "Authorization",
	"accept", "origin", "Cache-Control", "X-Requested-With",
}

// CORSOptions is a host's cross-origin policy.
type CORSOptions struct {
	// AllowOrigin lists the origins whose pages may call the API, as browsers
	// send them ("https://app.example.com", "app://localhost"); a request
	// from one is answered with its own origin. "*" admits every origin, which
	// Credentials rules out.
	AllowOrigin []string
	// AllowHeaders are the request headers the API reads beyond the base set.
	AllowHeaders []string
	// ExposeHeaders are the response headers its clients read.
	ExposeHeaders []string
	// Credentials lets a page send its cookies and HTTP authentication.
	Credentials bool
}

// CORS wraps next with the host's cross-origin policy, short-circuiting
// OPTIONS preflights with 204 before they reach next. Apply it at the
// http.Server handler so preflights bypass the app's routes entirely. A
// request from an origin the policy does not admit gets no CORS headers, and
// its browser keeps the response from the page. Credentials offered to any
// origin is a mistake in the program, and panics.
func CORS(opts CORSOptions, next http.Handler) http.Handler {
	anyOrigin := slices.Contains(opts.AllowOrigin, "*")
	if anyOrigin && opts.Credentials {
		panic("apiserver: CORS credentials for any origin")
	}
	allow := strings.Join(append(slices.Clone(baseAllowHeaders), opts.AllowHeaders...), ", ")
	expose := strings.Join(opts.ExposeHeaders, ", ")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Add("Vary", "Origin")
		if origin := r.Header.Get("Origin"); origin != "" && (anyOrigin || slices.Contains(opts.AllowOrigin, origin)) {
			if anyOrigin {
				h.Set("Access-Control-Allow-Origin", "*")
			} else {
				h.Set("Access-Control-Allow-Origin", origin)
			}
			if opts.Credentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}
			h.Set("Access-Control-Allow-Headers", allow)
			if expose != "" {
				h.Set("Access-Control-Expose-Headers", expose)
			}
			h.Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
