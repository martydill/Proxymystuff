package main

import (
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestTargetResolverHeader(t *testing.T) {
	resolver := &TargetResolver{}
	req := &http.Request{Header: http.Header{}, URL: &url.URL{Path: "/api"}}
	req.Header.Set("X-Proxy-Target", "https://example.com")

	target, useRequestPath, err := resolver.Resolve(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !useRequestPath {
		t.Fatalf("expected request path to be used")
	}
	if target.Host != "example.com" {
		t.Fatalf("unexpected target host: %s", target.Host)
	}
}

func TestTargetResolverQuery(t *testing.T) {
	resolver := &TargetResolver{}
	req := &http.Request{Header: http.Header{}, URL: &url.URL{Path: "/api", RawQuery: "target=https://example.com&foo=bar"}}

	target, _, err := resolver.Resolve(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.Host != "example.com" {
		t.Fatalf("unexpected target host: %s", target.Host)
	}
	if got := req.URL.RawQuery; got != "foo=bar" {
		t.Fatalf("expected target to be stripped from query, got %q", got)
	}
}

func TestTargetResolverProxyPath(t *testing.T) {
	resolver := &TargetResolver{}
	encoded := url.PathEscape("https://example.com/base")
	req := &http.Request{Header: http.Header{}, URL: &url.URL{Path: "/proxy/" + encoded}}

	target, useRequestPath, err := resolver.Resolve(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if useRequestPath {
		t.Fatalf("expected request path to be ignored")
	}
	if target.Path != "/base" {
		t.Fatalf("unexpected target path: %s", target.Path)
	}
}

func TestJoinURLPath(t *testing.T) {
	cases := []struct {
		base string
		path string
		want string
	}{
		{"", "/api", "/api"},
		{"/base", "/api", "/base/api"},
		{"/base/", "/api", "/base/api"},
		{"/base", "api", "/base/api"},
	}

	for _, c := range cases {
		if got := joinURLPath(c.base, c.path); got != c.want {
			t.Fatalf("joinURLPath(%q, %q) = %q, want %q", c.base, c.path, got, c.want)
		}
	}
}

func TestGzipResponseCapture(t *testing.T) {
	store := NewLogStore(10)
	resolver := &TargetResolver{DefaultTarget: nil}
	handler := &ProxyHandler{Store: store, Resolver: resolver}
	server := httptest.NewServer(handler)
	defer server.Close()

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)

		gw := gzip.NewWriter(w)
		defer gw.Close()
		_, _ = gw.Write([]byte("Hello Gzip World"))
	}))
	defer targetServer.Close()

	req, _ := http.NewRequest("GET", server.URL, nil)
	req.Header.Set("X-Proxy-Target", targetServer.URL)
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip response from target")
	}

	entries := store.List()
	if len(entries) == 0 {
		t.Fatal("no logs recorded")
	}
	last := entries[0]

	if last.ResponseBodyEncoding != "utf-8" {
		t.Logf("Current encoding: %s", last.ResponseBodyEncoding)
		t.Logf("Current body start: %s", last.ResponseBody[:10])
		t.Error("expected decoded utf-8 body for gzip response")
	}

	if !strings.Contains(last.ResponseBody, "Hello Gzip World") {
		t.Errorf("expected body to contain 'Hello Gzip World', got: %s", last.ResponseBody)
	}
}
