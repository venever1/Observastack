package observability

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"observastack/internal/requestctx"
)

// entryFrom runs fn and decodes the single JSON line the logger wrote. A panic
// inside fn fails the test rather than escaping to the test runner.
func entryFrom(t *testing.T, logger *Logger, fn func()) LogEntry {
	t.Helper()

	var buf strings.Builder
	logger.SetOutput(&buf)

	fn()

	var entry LogEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &entry); err != nil {
		t.Fatalf("decode log entry %q: %v", buf.String(), err)
	}
	return entry
}

func TestLogger_newEntry_ReadsStringTenantID(t *testing.T) {
	logger := NewLogger()
	ctx := requestctx.WithTenantID(context.Background(), "tenant-abc")

	entry := entryFrom(t, logger, func() {
		logger.Info(ctx, "INFO", "GET", "/things", 200, time.Millisecond, "request completed")
	})

	if entry.TenantID != "tenant-abc" {
		t.Fatalf("got tenant_id %q, want %q", entry.TenantID, "tenant-abc")
	}
}

// A plain string key spelling "tenantID" must not be readable, or any package
// could forge tenant attribution in the access log. This is the case the
// unexported key type exists to prevent.
//
// The nolint is deliberate: the test has to use a bare string key precisely
// because proving that key is inert is the point. This is the only reason
// SA1029 is silenced anywhere in the repo, and it silences one test rather than
// a package.
func TestLogger_newEntry_PlainStringKeyIsNotTenant(t *testing.T) {
	logger := NewLogger()
	//nolint:staticcheck // SA1029: the bare string key is the subject under test.
	ctx := context.WithValue(context.Background(), "tenantID", "tenant-forged")

	entry := entryFrom(t, logger, func() {
		logger.Info(ctx, "INFO", "GET", "/things", 200, time.Millisecond, "request completed")
	})

	if entry.TenantID != "" {
		t.Fatalf("got tenant_id %q, want empty; a plain string key must not forge a tenant", entry.TenantID)
	}
}

// A non-string tenant must not panic the logger. A bare type assertion here
// would turn a bad context value into a failure of whatever the handler was
// doing. Only requestctx's own package can put a wrong-typed value under the
// real key, so this exercises the reader with a string key holding a non-string.
func TestLogger_newEntry_NonStringTenantIDDoesNotPanic(t *testing.T) {
	for name, value := range map[string]any{
		"int":    42,
		"struct": struct{ ID string }{ID: "tenant-abc"},
		"slice":  []string{"tenant-abc"},
		"bool":   true,
	} {
		t.Run(name, func(t *testing.T) {
			logger := NewLogger()
			//nolint:staticcheck // SA1029: bare string key, as in the tests above.
			ctx := context.WithValue(context.Background(), "tenantID", value)

			entry := entryFrom(t, logger, func() {
				logger.Info(ctx, "INFO", "GET", "/things", 200, time.Millisecond, "request completed")
			})

			if entry.TenantID != "" {
				t.Fatalf("got tenant_id %q, want empty for a non-string value", entry.TenantID)
			}
			// The value itself must not leak into the log.
			if strings.Contains(strings.Join([]string{entry.Message, entry.Path, entry.Method}, " "), "tenant-abc") {
				t.Fatal("log must not echo the non-string context value")
			}
		})
	}
}

func TestLogger_newEntry_MissingTenantIDIsEmpty(t *testing.T) {
	logger := NewLogger()

	entry := entryFrom(t, logger, func() {
		logger.Info(context.Background(), "INFO", "GET", "/things", 200, time.Millisecond, "request completed")
	})

	if entry.TenantID != "" {
		t.Fatalf("got tenant_id %q, want empty when unset", entry.TenantID)
	}
}

// The Error path shares newEntry, so it needs the same guard.
func TestLogger_Error_NonStringTenantIDDoesNotPanic(t *testing.T) {
	logger := NewLogger()
	//nolint:staticcheck // SA1029: bare string key, as in the test above.
	ctx := context.WithValue(context.Background(), "tenantID", 42)

	entry := entryFrom(t, logger, func() {
		logger.Error(ctx, "GET", "/things", 500, time.Millisecond, "internal server error", context.Canceled)
	})

	if entry.TenantID != "" {
		t.Fatalf("got tenant_id %q, want empty for a non-string value", entry.TenantID)
	}
	if entry.Error != context.Canceled.Error() {
		t.Fatalf("got error %q, want %q", entry.Error, context.Canceled.Error())
	}
}
