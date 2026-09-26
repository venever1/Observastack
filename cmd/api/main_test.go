package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewServer_SetsDefensiveTimeouts(t *testing.T) {
	srv := newServer(":0", http.NotFoundHandler())

	cases := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{name: "ReadHeaderTimeout", got: srv.ReadHeaderTimeout, want: 5 * time.Second},
		{name: "ReadTimeout", got: srv.ReadTimeout, want: 10 * time.Second},
		{name: "WriteTimeout", got: srv.WriteTimeout, want: 10 * time.Second},
		{name: "IdleTimeout", got: srv.IdleTimeout, want: 120 * time.Second},
	}

	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

func TestNewServer_NoTimeoutLeftUnset(t *testing.T) {
	srv := newServer(":0", http.NotFoundHandler())

	// A zero value on any of these means "no limit" in net/http, which is the
	// exact Slowloris exposure this server is meant to close.
	if srv.ReadTimeout == 0 {
		t.Error("ReadTimeout must not be zero")
	}
	if srv.ReadHeaderTimeout == 0 {
		t.Error("ReadHeaderTimeout must not be zero")
	}
	if srv.WriteTimeout == 0 {
		t.Error("WriteTimeout must not be zero")
	}
	if srv.IdleTimeout == 0 {
		t.Error("IdleTimeout must not be zero")
	}
}

func TestNewServer_PreservesAddrAndHandler(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	srv := newServer(":9999", handler)

	if srv.Addr != ":9999" {
		t.Errorf("Addr = %q, want %q", srv.Addr, ":9999")
	}
	if srv.Handler == nil {
		t.Fatal("Handler must not be nil")
	}

	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))

	if rec.Code != http.StatusTeapot {
		t.Errorf("handler passthrough status = %d, want %d", rec.Code, http.StatusTeapot)
	}
}
