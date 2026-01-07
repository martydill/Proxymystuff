package main

import (
	"net/http"
	"net/url"
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
