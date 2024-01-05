package main

import (
	"log"
	"net/http"
	"net/url"

	"golang.org/x/net/webdav"
)

type httpHandler struct {
	webdav.Handler
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Wrap writer
	lw := &logResponseWriter{
		ResponseWriter: w,
		method:         r.Method,
		url:            r.URL.String(),
	}
	url, err := url.QueryUnescape(lw.url)
	if err == nil {
		lw.url = url
	}
	h.Handler.ServeHTTP(lw, r)
}

type logResponseWriter struct {
	http.ResponseWriter
	method  string
	url     string
	written bool
}

func (w *logResponseWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
		w.written = true
	}
	return w.ResponseWriter.Write(b)
}

func (w *logResponseWriter) WriteHeader(statusCode int) {
	w.ResponseWriter.WriteHeader(statusCode)
	w.written = true
	text := http.StatusText(statusCode)
	log.Println(w.method, w.url, statusCode, text)
}
