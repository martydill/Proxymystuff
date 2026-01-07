package main

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	defaultLogLimit = 1000
	maxBodyLogSize  = 64 * 1024
)

//go:embed web/*
var webAssets embed.FS

func main() {
	var listenAddr string
	var defaultTarget string
	var logLimit int

	flag.StringVar(&listenAddr, "listen", ":8080", "address to listen on")
	flag.StringVar(&defaultTarget, "default-target", "", "default target base URL for proxying")
	flag.IntVar(&logLimit, "log-limit", defaultLogLimit, "maximum number of log entries to retain")
	flag.Parse()

	var defaultTargetURL *url.URL
	if defaultTarget != "" {
		parsed, err := url.Parse(defaultTarget)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			log.Fatalf("invalid default target: %s", defaultTarget)
		}
		defaultTargetURL = parsed
	}

	store := NewLogStore(logLimit)
	resolver := &TargetResolver{DefaultTarget: defaultTargetURL}

	webFS, err := fs.Sub(webAssets, "web")
	if err != nil {
		log.Fatalf("failed to load embedded assets: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/ui/", http.StripPrefix("/ui/", http.FileServer(http.FS(webFS))))
	mux.HandleFunc("/ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusFound)
	})
	mux.HandleFunc("/api/logs", handleListLogs(store))
	mux.HandleFunc("/api/logs/", handleGetLog(store))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	proxy := &ProxyHandler{Store: store, Resolver: resolver}
	mux.Handle("/", proxy)

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           loggingMiddleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("listening on %s", listenAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
}

type TargetResolver struct {
	DefaultTarget *url.URL
}

func (r *TargetResolver) Resolve(req *http.Request) (*url.URL, bool, error) {
	if target := req.Header.Get("X-Proxy-Target"); target != "" {
		return parseTarget(target, req, true)
	}

	query := req.URL.Query()
	if target := query.Get("target"); target != "" {
		query.Del("target")
		req.URL.RawQuery = query.Encode()
		return parseTarget(target, req, true)
	}

	if strings.HasPrefix(req.URL.Path, "/proxy/") {
		trimmed := strings.TrimPrefix(req.URL.Path, "/proxy/")
		decoded, err := url.PathUnescape(trimmed)
		if err != nil {
			return nil, false, fmt.Errorf("invalid proxy path: %w", err)
		}
		return parseTarget(decoded, req, false)
	}

	if r.DefaultTarget != nil {
		return parseTarget(r.DefaultTarget.String(), req, true)
	}

	return nil, false, errors.New("no target specified")
}

func parseTarget(target string, req *http.Request, useRequestPath bool) (*url.URL, bool, error) {
	parsed, err := url.Parse(target)
	if err != nil {
		return nil, false, fmt.Errorf("invalid target: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, false, errors.New("target must include scheme and host")
	}
	return parsed, useRequestPath, nil
}

type ProxyHandler struct {
	Store    *LogStore
	Resolver *TargetResolver
}

func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	entry := h.Store.NewEntry(r)

	target, useRequestPath, err := h.Resolver.Resolve(r)
	if err != nil {
		entry.SetError(err.Error())
		entry.SetDurationSinceStart()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	requestBody, err := io.ReadAll(r.Body)
	if err != nil {
		entry.SetError(fmt.Sprintf("read request body: %v", err))
		entry.SetDurationSinceStart()
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()
	entry.SetRequestBody(requestBody)
	r.Body = io.NopCloser(bytes.NewReader(requestBody))

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host

			if useRequestPath {
				rawQuery := req.URL.RawQuery
				if target.RawQuery != "" {
					if rawQuery != "" {
						rawQuery = target.RawQuery + "&" + rawQuery
					} else {
						rawQuery = target.RawQuery
					}
				}
				req.URL.Path = joinURLPath(target.Path, req.URL.Path)
				req.URL.RawQuery = rawQuery
			} else {
				resolved := *target
				if resolved.RawQuery == "" {
					resolved.RawQuery = req.URL.RawQuery
				}
				req.URL.Path = resolved.Path
				req.URL.RawPath = resolved.RawPath
				req.URL.RawQuery = resolved.RawQuery
			}

			req.Host = target.Host
			req.Header.Del("X-Proxy-Target")
		},
		ModifyResponse: func(resp *http.Response) error {
			body, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				entry.SetError(fmt.Sprintf("read response body: %v", readErr))
				return readErr
			}
			_ = resp.Body.Close()
			entry.SetResponse(resp, body)
			resp.Body = io.NopCloser(bytes.NewReader(body))
			return nil
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, proxyErr error) {
			entry.SetError(proxyErr.Error())
			entry.SetDurationSinceStart()
			http.Error(rw, proxyErr.Error(), http.StatusBadGateway)
		},
	}

	entry.SetTarget(target.String())
	proxy.ServeHTTP(w, r)
	entry.SetDurationSinceStart()
}

type LogEntry struct {
	ID                    int64             `json:"id"`
	StartedAt             time.Time         `json:"startedAt"`
	DurationMillis        int64             `json:"durationMillis"`
	ClientIP              string            `json:"clientIp"`
	Method                string            `json:"method"`
	URL                   string            `json:"url"`
	Target                string            `json:"target"`
	Status                int               `json:"status"`
	RequestHeaders        map[string]string `json:"requestHeaders"`
	ResponseHeaders       map[string]string `json:"responseHeaders"`
	RequestBody           string            `json:"requestBody"`
	RequestBodyEncoding   string            `json:"requestBodyEncoding"`
	RequestBodyTruncated  bool              `json:"requestBodyTruncated"`
	ResponseBody          string            `json:"responseBody"`
	ResponseBodyEncoding  string            `json:"responseBodyEncoding"`
	ResponseBodyTruncated bool              `json:"responseBodyTruncated"`
	Error                 string            `json:"error,omitempty"`
	RequestContentType    string            `json:"requestContentType"`
	ResponseContentType   string            `json:"responseContentType"`
	RequestContentLength  int64             `json:"requestContentLength"`
	ResponseContentLength int64             `json:"responseContentLength"`

	mu sync.Mutex
}

type LogEntryView struct {
	ID                    int64             `json:"id"`
	StartedAt             time.Time         `json:"startedAt"`
	DurationMillis        int64             `json:"durationMillis"`
	ClientIP              string            `json:"clientIp"`
	Method                string            `json:"method"`
	URL                   string            `json:"url"`
	Target                string            `json:"target"`
	Status                int               `json:"status"`
	RequestHeaders        map[string]string `json:"requestHeaders"`
	ResponseHeaders       map[string]string `json:"responseHeaders"`
	RequestBody           string            `json:"requestBody"`
	RequestBodyEncoding   string            `json:"requestBodyEncoding"`
	RequestBodyTruncated  bool              `json:"requestBodyTruncated"`
	ResponseBody          string            `json:"responseBody"`
	ResponseBodyEncoding  string            `json:"responseBodyEncoding"`
	ResponseBodyTruncated bool              `json:"responseBodyTruncated"`
	Error                 string            `json:"error,omitempty"`
	RequestContentType    string            `json:"requestContentType"`
	ResponseContentType   string            `json:"responseContentType"`
	RequestContentLength  int64             `json:"requestContentLength"`
	ResponseContentLength int64             `json:"responseContentLength"`
}

func (e *LogEntry) SetTarget(target string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Target = target
}

func (e *LogEntry) SetRequestBody(body []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.RequestContentLength = int64(len(body))
	e.RequestContentType = http.DetectContentType(body)
	e.RequestBody, e.RequestBodyEncoding, e.RequestBodyTruncated = formatBody(body)
}

func (e *LogEntry) SetResponse(resp *http.Response, body []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Status = resp.StatusCode
	e.ResponseContentLength = int64(len(body))
	e.ResponseContentType = resp.Header.Get("Content-Type")
	e.ResponseHeaders = flattenHeaders(resp.Header)

	bodyToFormat := decodeResponseBody(resp.Header, body)

	e.ResponseBody, e.ResponseBodyEncoding, e.ResponseBodyTruncated = formatBody(bodyToFormat)
}

func (e *LogEntry) SetError(err string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Error = err
}

func (e *LogEntry) SetDurationSinceStart() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.DurationMillis = time.Since(e.StartedAt).Milliseconds()
}

func (e *LogEntry) Snapshot() LogEntryView {
	e.mu.Lock()
	defer e.mu.Unlock()
	return LogEntryView{
		ID:                    e.ID,
		StartedAt:             e.StartedAt,
		DurationMillis:        e.DurationMillis,
		ClientIP:              e.ClientIP,
		Method:                e.Method,
		URL:                   e.URL,
		Target:                e.Target,
		Status:                e.Status,
		RequestHeaders:        cloneMap(e.RequestHeaders),
		ResponseHeaders:       cloneMap(e.ResponseHeaders),
		RequestBody:           e.RequestBody,
		RequestBodyEncoding:   e.RequestBodyEncoding,
		RequestBodyTruncated:  e.RequestBodyTruncated,
		ResponseBody:          e.ResponseBody,
		ResponseBodyEncoding:  e.ResponseBodyEncoding,
		ResponseBodyTruncated: e.ResponseBodyTruncated,
		Error:                 e.Error,
		RequestContentType:    e.RequestContentType,
		ResponseContentType:   e.ResponseContentType,
		RequestContentLength:  e.RequestContentLength,
		ResponseContentLength: e.ResponseContentLength,
	}
}

type LogStore struct {
	mu      sync.Mutex
	limit   int
	nextID  int64
	entries []*LogEntry
	index   map[int64]*LogEntry
}

func NewLogStore(limit int) *LogStore {
	if limit <= 0 {
		limit = defaultLogLimit
	}
	return &LogStore{
		limit: limit,
		index: make(map[int64]*LogEntry),
	}
}

func (s *LogStore) NewEntry(r *http.Request) *LogEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	entry := &LogEntry{
		ID:             s.nextID,
		StartedAt:      time.Now(),
		ClientIP:       clientIP(r),
		Method:         r.Method,
		URL:            r.URL.String(),
		RequestHeaders: flattenHeaders(r.Header),
	}
	s.entries = append(s.entries, entry)
	s.index[entry.ID] = entry

	if len(s.entries) > s.limit {
		oldest := s.entries[0]
		delete(s.index, oldest.ID)
		s.entries = s.entries[1:]
	}

	return entry
}

func (s *LogStore) List() []LogEntryView {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]LogEntryView, 0, len(s.entries))
	for i := len(s.entries) - 1; i >= 0; i-- {
		result = append(result, s.entries[i].Snapshot())
	}
	return result
}

func (s *LogStore) Get(id int64) (LogEntryView, bool) {
	s.mu.Lock()
	entry, ok := s.index[id]
	s.mu.Unlock()
	if !ok {
		return LogEntryView{}, false
	}
	return entry.Snapshot(), true
}

func handleListLogs(store *LogStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries := store.List()
		respondJSON(w, entries)
	}
}

func handleGetLog(store *LogStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.TrimPrefix(r.URL.Path, "/api/logs/")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid log id", http.StatusBadRequest)
			return
		}
		entry, ok := store.Get(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		respondJSON(w, entry)
	}
}

func respondJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func flattenHeaders(headers http.Header) map[string]string {
	flat := make(map[string]string, len(headers))
	for key, values := range headers {
		flat[key] = strings.Join(values, ", ")
	}
	return flat
}

func cloneMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func formatBody(body []byte) (string, string, bool) {
	truncated := false
	if len(body) > maxBodyLogSize {
		body = body[:maxBodyLogSize]
		truncated = true
	}

	if utf8.Valid(body) {
		return string(body), "utf-8", truncated
	}

	if truncated {
		// If truncated, we might have split a multi-byte character.
		// Try removing up to 3 bytes from the end to see if it becomes valid.
		for i := 1; i <= 3 && len(body) > i; i++ {
			sub := body[:len(body)-i]
			if utf8.Valid(sub) {
				return string(sub), "utf-8", truncated
			}
		}
	}

	encoded := base64.StdEncoding.EncodeToString(body)
	return encoded, "base64", truncated
}

func decodeResponseBody(headers http.Header, body []byte) []byte {
	if len(body) == 0 {
		return body
	}

	if isGzipEncoded(headers) || isGzipData(body) {
		if decoded, err := gunzip(body); err == nil {
			return decoded
		}
	}

	return body
}

func isGzipEncoded(headers http.Header) bool {
	encoding := strings.ToLower(headers.Get("Content-Encoding"))
	return strings.Contains(encoding, "gzip")
}

func isGzipData(body []byte) bool {
	return len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b
}

func gunzip(body []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func joinURLPath(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}
