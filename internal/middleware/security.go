// Package middleware provides cross-cutting HTTP middleware that is not tied to
// a single business domain.
package middleware

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

const (
	headerOrigin = "Origin"

	headerACAllowOrigin      = "Access-Control-Allow-Origin"
	headerACAllowMethods     = "Access-Control-Allow-Methods"
	headerACAllowHeaders     = "Access-Control-Allow-Headers"
	headerACAllowCredentials = "Access-Control-Allow-Credentials"
	headerACMaxAge           = "Access-Control-Max-Age"
	headerVary               = "Vary"

	envAllowedOrigins = "ALLOWED_ORIGINS"
)

// corsMaxAgeSeconds caps how long a browser may cache a preflight result.
const corsMaxAgeSeconds = "600"

// corsAllowMethods lists the methods this API exposes. It is intentionally a
// fixed list rather than reflecting the request, so a preflight cannot be used
// to discover or enable methods the API does not implement.
const corsAllowMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"

// corsAllowHeaders lists request headers a browser client may send.
// Authorization is required because this API authenticates with a bearer token.
const corsAllowHeaders = "Authorization, Content-Type"

// Security headers. This API serves JSON only: no HTML, no templates, no
// static assets are served from any handler.
const (
	headerXContentTypeOptions = "X-Content-Type-Options"
	headerXFrameOptions       = "X-Frame-Options"
	headerStrictTransport     = "Strict-Transport-Security"
	headerContentSecurityPol  = "Content-Security-Policy"
	headerReferrerPolicy      = "Referrer-Policy"

	valueNoSniff = "nosniff"
	valueDeny    = "DENY"
	// Two years, the value recommended by the OWASP Secure Headers project.
	valueHSTS = "max-age=63072000; includeSubDomains"
	// "none" rather than "self": this API only ever emits JSON and serves no
	// HTML, static assets or API docs, so no resource type needs to be allowed.
	// It is also strictly tighter, since it forbids every fetch destination
	// rather than only cross-origin ones. Revisit if a Swagger UI or any other
	// document-rendering endpoint is ever served.
	valueCSP      = "default-src 'none'"
	valueReferrer = "no-referrer"
)

// ParseAllowedOrigins splits a comma-separated origin list.
//
// A wildcard is rejected rather than accepted. This API currently authenticates
// with a bearer token rather than a cookie, so "*" would not be immediately
// exploitable, but refusing it here means adding cookie-based auth later cannot
// silently inherit a wildcard that is already in the environment.
func ParseAllowedOrigins(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	var origins []string
	for _, part := range strings.Split(raw, ",") {
		origin := strings.TrimSpace(part)
		if origin == "" {
			continue
		}
		if origin == "*" {
			return nil, fmt.Errorf(
				"%s must not contain a wildcard \"*\"; list explicit origins instead", envAllowedOrigins)
		}
		origins = append(origins, strings.TrimSuffix(origin, "/"))
	}

	return origins, nil
}

// AllowedOriginsFromEnv reads and validates ALLOWED_ORIGINS. An unset variable
// is valid and yields an empty list, which means cross-origin requests are not
// permitted; it never falls back to a wildcard.
func AllowedOriginsFromEnv() ([]string, error) {
	return ParseAllowedOrigins(os.Getenv(envAllowedOrigins))
}

// SecurityHeaders sets defensive headers on every response.
//
// It must be the outermost middleware so the headers are present on every
// outcome: preflight rejections, 413 from the body limit, handler errors and
// recovered panics. Headers are set before delegating, so they are already
// written by the time a downstream handler commits a status code.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set(headerXContentTypeOptions, valueNoSniff)
		h.Set(headerXFrameOptions, valueDeny)
		h.Set(headerStrictTransport, valueHSTS)
		h.Set(headerContentSecurityPol, valueCSP)
		h.Set(headerReferrerPolicy, valueReferrer)

		next.ServeHTTP(w, r)
	})
}

// CORS restricts cross-origin access to an explicit allowlist.
//
// A preflight is answered here and never reaches the router. Origin is added to
// Vary so shared caches do not serve one origin's response to another.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get(headerOrigin)

			// Same-origin and non-browser clients send no Origin; pass through.
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			h := w.Header()
			h.Add(headerVary, headerOrigin)

			if _, ok := allowed[origin]; !ok {
				// Not allowlisted: answer without any CORS grant so the browser
				// blocks the response. A preflight gets a bare 403.
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			h.Set(headerACAllowOrigin, origin)
			// Credentials are deliberately not enabled: this API uses bearer
			// tokens, so there is no cookie to expose.
			h.Del(headerACAllowCredentials)

			if r.Method == http.MethodOptions {
				h.Set(headerACAllowMethods, corsAllowMethods)
				h.Set(headerACAllowHeaders, corsAllowHeaders)
				h.Set(headerACMaxAge, corsMaxAgeSeconds)
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
