package server_test

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"testing"

	"racing/internal/servertest"
)

func TestPagesServedGzipped(t *testing.T) {
	h := servertest.Start(t)
	for _, path := range []string{"/play", "/host"} {
		resp := h.Get(path, http.Header{"Accept-Encoding": {"gzip, deflate, br"}})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d", path, resp.StatusCode)
		}
		if ce := resp.Header.Get("Content-Encoding"); ce != "gzip" {
			t.Fatalf("%s: Content-Encoding %q, want gzip", path, ce)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s: Content-Type %q, want text/html", path, ct)
		}
		zr, err := gzip.NewReader(resp.Body)
		if err != nil {
			t.Fatalf("%s: not gzip: %v", path, err)
		}
		body, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if !strings.HasPrefix(strings.ToLower(string(body)), "<!doctype html>") {
			t.Errorf("%s: body does not start with doctype: %.40q", path, body)
		}
	}
}

func TestPagesServedPlainWithoutGzip(t *testing.T) {
	h := servertest.Start(t)
	resp := h.Get("/play", nil)
	if ce := resp.Header.Get("Content-Encoding"); ce != "" {
		t.Fatalf("Content-Encoding %q, want none", ce)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.HasPrefix(strings.ToLower(string(body)), "<!doctype html>") {
		t.Errorf("body does not start with doctype: %.40q", body)
	}
}

func TestPlayPageFitsGzipBudget(t *testing.T) {
	const budget = 10 * 1024
	h := servertest.Start(t)
	resp := h.Get("/play", http.Header{"Accept-Encoding": {"gzip"}})
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatal("/play not gzipped")
	}
	b, _ := io.ReadAll(resp.Body)
	if len(b) >= budget {
		t.Errorf("/play is %d bytes gzipped, budget is < %d", len(b), budget)
	}
}

func TestRootRedirectsToPlay(t *testing.T) {
	h := servertest.Start(t)
	rec := h.ServeFrom("203.0.113.9:443", "/", nil)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/play" {
		t.Errorf("/ -> %d %q, want 302 /play", rec.Code, rec.Header().Get("Location"))
	}
}
