package server

import (
	"bytes"
	"compress/gzip"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

// page serves a static HTML page, gzip-compressed once up front and sent
// compressed to every client that accepts gzip.
func page(body []byte) http.Handler {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.Write(body)
	zw.Close()
	gz := buf.Bytes()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Type", "text/html; charset=utf-8")
		hd.Set("Cache-Control", "no-cache")
		hd.Add("Vary", "Accept-Encoding")
		b := body
		if acceptsGzip(r) {
			hd.Set("Content-Encoding", "gzip")
			b = gz
		}
		hd.Set("Content-Length", strconv.Itoa(len(b)))
		w.Write(b)
	})
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(coding), "gzip") {
			continue
		}
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			q, err := strconv.ParseFloat(v, 64)
			return err == nil && q > 0
		}
		return true
	}
	return false
}

// hostOnly guards host-screen endpoints: only direct loopback requests are
// allowed. Requests relayed by the Cloudflare tunnel also arrive from
// loopback, so they are recognised by their Cf-Connecting-IP header.
func hostOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocal(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLocal(r *http.Request) bool {
	if r.Header.Get("Cf-Connecting-Ip") != "" {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}
