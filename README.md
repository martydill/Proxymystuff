# Proxy My Stuff

A lightweight Go reverse proxy and traffic inspector. Here's an example of pointing Claude Code at it:

<img width="1277" height="801" alt="image" src="https://github.com/user-attachments/assets/09457917-6267-4294-8f2f-a529b7868bad" />

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
