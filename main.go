package main

import (
	"embed"
	"log"
	"strings"
	"time"

	"github.com/valyala/fasthttp"
)

//go:embed web/dist
var webFS embed.FS

const (
	userAgent   = "KoroneProxy"
	targetHost  = "www.pekora.zip"
	baseTarget  = "https://" + targetHost
	apiPrefix   = "/apisite/"
	healthPath  = "/healthz"
	port        = ":8080"
	timeout     = 10 * time.Second
	maxRetries  = 2
	retryDelay  = 30 * time.Millisecond
)

// Headers that must never be forwarded, either because they're hop-by-hop
// (RFC 7230 §6.1) or because forwarding them would break the proxied
// request/response. Keys are canonical MIME header case since the client
// and server both normalize header names by default.
var strippedRequestHeaders = map[string]bool{
	"Host":       true,
	"Roblox-Id":  true,
	"User-Agent": true,
}

var strippedResponseHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
	"Content-Encoding":    true, // client auto-decompresses; body no longer matches this header
}

var upstreamClient = &fasthttp.Client{
	ReadTimeout:              timeout,
	WriteTimeout:             timeout,
	MaxIdleConnDuration:      90 * time.Second,
	MaxConnsPerHost:          1024,
	NoDefaultUserAgentHeader: true,
	DisablePathNormalizing:   true,
}

const staticRoot = "web/dist"

func main() {
	srv := &fasthttp.Server{
		Handler:      router,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
		IdleTimeout:  60 * time.Second,
	}
	log.Printf("KoroneProxy listening on %s", port)
	if err := srv.ListenAndServe(port); err != nil {
		log.Fatalf("ListenAndServe: %s", err)
	}
}

func router(ctx *fasthttp.RequestCtx) {
	path := string(ctx.Path())

	switch {
	case path == healthPath:
		handleHealth(ctx)
	case strings.HasPrefix(path, apiPrefix):
		handleProxy(ctx, path)
	default:
		handleStatic(ctx, path)
	}
}

func handleHealth(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType("application/json")
	ctx.SetBodyString(`{"status":"ok"}`)
}

func handleProxy(ctx *fasthttp.RequestCtx, path string) {
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	buildUpstreamRequest(req, ctx, path)

	if err := doWithRetry(req, resp); err != nil {
		ctx.SetStatusCode(fasthttp.StatusBadGateway)
		ctx.SetBodyString("Proxy failed to connect. Please try again.")
		return
	}

	writeUpstreamResponse(ctx, resp)
}

func buildUpstreamRequest(req *fasthttp.Request, ctx *fasthttp.RequestCtx, path string) {
	target := baseTarget + path
	if qs := ctx.QueryArgs().QueryString(); len(qs) > 0 {
		target += "?" + string(qs)
	}

	req.SetRequestURI(target)
	req.Header.SetMethod(string(ctx.Method()))
	req.SetHost(targetHost)
	req.Header.Set("User-Agent", userAgent)

	if body := ctx.Request.Body(); len(body) > 0 {
		req.SetBody(body)
	}

	ctx.Request.Header.VisitAll(func(k, v []byte) {
		if strippedRequestHeaders[string(k)] {
			return
		}
		req.Header.SetBytesKV(k, v)
	})
}

func doWithRetry(req *fasthttp.Request, resp *fasthttp.Response) error {
	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			resp.Reset()
			time.Sleep(time.Duration(attempt) * retryDelay)
		}

		err = upstreamClient.DoTimeout(req, resp, timeout)
		if err == nil {
			return nil
		}
		if err == fasthttp.ErrTimeout {
			return err // upstream is slow, not transiently down; retrying won't help
		}
	}
	return err
}

func writeUpstreamResponse(ctx *fasthttp.RequestCtx, resp *fasthttp.Response) {
	resp.Header.VisitAll(func(k, v []byte) {
		if strippedResponseHeaders[string(k)] {
			return
		}
		ctx.Response.Header.SetBytesKV(k, v)
	})

	body, err := resp.BodyUncompressed()
	if err != nil {
		body = resp.Body()
	}

	ctx.SetStatusCode(resp.StatusCode())
	ctx.SetBody(body)
}

// handleStatic serves files embedded under web/dist, falling back to
// index.html for unknown paths so client-side routing keeps working.
func handleStatic(ctx *fasthttp.RequestCtx, path string) {
	file := strings.TrimPrefix(path, "/")
	if file == "" {
		file = "index.html"
	}

	data, err := webFS.ReadFile(staticRoot + "/" + file)
	if err != nil {
		file = "index.html"
		if data, err = webFS.ReadFile(staticRoot + "/" + file); err != nil {
			ctx.SetStatusCode(fasthttp.StatusNotFound)
			return
		}
	}

	ctx.Response.Header.Set("Cache-Control", cacheControlFor(file))
	ctx.SetContentType(contentTypeFor(file))
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(data)
}

// cacheControlFor returns a long cache lifetime for hashed static assets
// and a short one for index.html, which changes on every deploy.
func cacheControlFor(file string) string {
	if file == "index.html" {
		return "no-cache"
	}
	return "public, max-age=600"
}

func contentTypeFor(file string) string {
	switch {
	case strings.HasSuffix(file, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(file, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(file, ".js"):
		return "application/javascript; charset=utf-8"
	case strings.HasSuffix(file, ".json"):
		return "application/json; charset=utf-8"
	case strings.HasSuffix(file, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(file, ".png"):
		return "image/png"
	case strings.HasSuffix(file, ".ico"):
		return "image/x-icon"
	case strings.HasSuffix(file, ".wasm"):
		return "application/wasm"
	default:
		return "application/octet-stream"
	}
}