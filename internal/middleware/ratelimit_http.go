package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// CodeRateLimited is the API_SPEC error code for a throttled request.
const CodeRateLimited = "RATE_LIMITED"

// subjectBodyLimit caps how much of a request body is buffered to derive a
// per-subject rate limit key. Credentials are small; anything larger is not
// worth inspecting and falls back to per-IP limiting only.
const subjectBodyLimit = 8 << 10

// ClientIP resolves the caller's address for rate limiting.
//
// X-Forwarded-For is client-controlled, so honouring it unconditionally would
// let an attacker mint a fresh identity per request and bypass every per-IP
// limit. It is therefore read only when trustProxy is true, meaning a proxy
// this deployment controls sits in front of the app.
//
// When the header is trusted, the right-most entry is used: a proxy appends the
// address it actually observed, so the last entry is the one we control and
// earlier hops cannot forge it.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if candidate := strings.TrimSpace(parts[len(parts)-1]); candidate != "" {
				if ip := net.ParseIP(candidate); ip != nil {
					return ip.String()
				}
			}
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	if net.ParseIP(host) != nil {
		return host
	}
	return r.RemoteAddr
}

// JSONSubject returns a function that pulls a string field out of a JSON body,
// for use as the per-subject rate limit key. An unparsable body yields an empty
// string, which leaves only the per-IP limit in force.
func JSONSubject(field string) func([]byte) string {
	return func(body []byte) string {
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(body, &payload); err != nil {
			return ""
		}
		raw, ok := payload[field]
		if !ok {
			return ""
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return ""
		}
		return value
	}
}

// RateLimit applies fixed-window rate limiting to a handler.
//
// ipLimiter and subjectLimiter may each be nil, in which case that dimension is
// skipped. A request must satisfy both: limiting only by IP lets a botnet grind
// a single account, and limiting only by subject lets one host spray many
// accounts.
//
// subjectFromBody receives a bounded prefix of the request body, which is then
// spliced back so the handler still decodes it in full. Pass nil to limit by IP
// only.
//
// A limiter that cannot reach Redis fails closed, because a rate limiter that
// silently stops working is an open door.
func RateLimit(
	ipLimiter, subjectLimiter *RateLimiter,
	trustProxy bool,
	subjectFromBody func([]byte) string,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ipLimiter != nil {
				allowed, retryAfter, err := ipLimiter.Allow(r.Context(), ClientIP(r, trustProxy))
				if err != nil || !allowed {
					writeRateLimitError(w, retryAfter)
					return
				}
			}

			if subjectLimiter != nil && subjectFromBody != nil {
				body, restore := peekBody(r)
				if restore != nil {
					defer restore()
				}

				if subject := subjectFromBody(body); subject != "" {
					allowed, retryAfter, err := subjectLimiter.Allow(r.Context(), subject)
					if err != nil || !allowed {
						writeRateLimitError(w, retryAfter)
						return
					}
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// peekBody reads a bounded prefix of the request body and replaces r.Body with
// a stream that replays those bytes ahead of the unread remainder, so the
// handler observes the body unmodified.
func peekBody(r *http.Request) ([]byte, func()) {
	if r.Body == nil {
		return nil, nil
	}

	original := r.Body
	prefix, err := io.ReadAll(io.LimitReader(original, subjectBodyLimit))
	r.Body = readCloser{
		Reader: io.MultiReader(bytes.NewReader(prefix), original),
		Closer: original,
	}

	if err != nil {
		// The body is already broken; leave it to the handler to report.
		return prefix, nil
	}
	return prefix, nil
}

type readCloser struct {
	io.Reader
	io.Closer
}

func writeRateLimitError(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int(retryAfter.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writePayloadError(w, http.StatusTooManyRequests, CodeRateLimited,
		"Too many requests, please retry later")
}
