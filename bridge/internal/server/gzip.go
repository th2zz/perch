package server

import (
	"compress/gzip"
	"net/http"
	"strings"
)

func gzipResponses(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") ||
			isWebSocketUpgrade(r) {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Encoding", "gzip")
		// Caches and proxies have to know the answer depends on the header, and the app's own client is a
		// cache of one: getting this wrong is how a phone ends up with a gzipped body it does not expect.
		w.Header().Add("Vary", "Accept-Encoding")
		writer := gzip.NewWriter(w)
		defer func() { _ = writer.Close() }()

		next.ServeHTTP(&gzipWriter{ResponseWriter: w, writer: writer}, r)
	})
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") ||
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// gzipWriter forwards headers through, but sends the body through the compressor.
//
// It deliberately does not implement http.Hijacker: a request that gets here is not an upgrade (see above),
// and a type that cannot be hijacked is a clearer failure than one that hijacks a compressed stream.
type gzipWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
}

func (g *gzipWriter) Write(p []byte) (int, error) {
	// Anything written before a handler sets a status defaults to 200, so the header has to go out first.
	g.Header().Del("Content-Length")
	return g.writer.Write(p)
}
