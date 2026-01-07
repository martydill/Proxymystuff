# Proxy My Stuff

A lightweight Go reverse proxy with a built-in web console for inspecting HTTP traffic. Point any local app at the proxy, forward requests to a target service, and browse full request/response logs (headers and bodies) in the UI.

## Features

- Reverse proxy with flexible target resolution.
- Configurable targets via header, query param, encoded URL path, or default target.
- Web UI to search, filter, and inspect individual HTTP messages.

## Running

```bash
go run .
```

Then open the UI at:

```
http://localhost:8080/ui/
```

### Configure the target

Pick one of the following options per request:

- **Header**: `X-Proxy-Target: https://example.com`
- **Query parameter**: `?target=https://example.com`
- **Path**: `/proxy/<url-encoded-target>`
- **Default target**: `go run . --default-target https://example.com`

Examples:

```bash
curl -H "X-Proxy-Target: https://httpbin.org" http://localhost:8080/anything
curl "http://localhost:8080/anything?target=https://httpbin.org"
curl "http://localhost:8080/proxy/https%3A%2F%2Fhttpbin.org%2Fanything"
```
