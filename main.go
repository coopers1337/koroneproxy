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
	userAgent  = "RoProxy"
	baseTarget = "https://www.pekora.zip"
	apiPrefix  = "/apisite/"
	port       = ":8080"
	timeout    = 10 * time.Second
	retries    = 3
)

var hopByHopHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
	"Content-Encoding":    true, // fasthttp client auto-decompresses; forwarding this mismatches the body
}

var client = &fasthttp.Client{
	ReadTimeout:              timeout,
	WriteTimeout:             timeout,
	MaxIdleConnDuration:      60 * time.Second,
	MaxConnsPerHost:          512,
	NoDefaultUserAgentHeader: true,
	DisablePathNormalizing:   true,
}

func main() {
	log.Printf("KoroneProxy listening on %s", port)
	if err := fasthttp.ListenAndServe(port, router); err != nil {
		log.Fatalf("ListenAndServe: %s", err)
	}
}

func router(ctx *fasthttp.RequestCtx) {
	path := string(ctx.Path())

	switch {
	case path == "/healthz":
		handleHealth(ctx)
	case strings.HasPrefix(path, apiPrefix):
		handleProxy(ctx, path)
	default:
		handleStatic(ctx, path)
	}
}

func handleHealth(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(`{"status":"ok"}`)
}

func handleProxy(ctx *fasthttp.RequestCtx, path string) {
	target := baseTarget + path
	if qs := ctx.QueryArgs().QueryString(); len(qs) > 0 {
		target += "?" + string(qs)
	}

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	buildUpstreamRequest(req, ctx, target)

	if err := doWithRetry(req, resp, retries); err != nil {
		ctx.SetStatusCode(fasthttp.StatusBadGateway)
		ctx.SetBodyString("Proxy failed to connect. Please try again.")
		return
	}

	writeUpstreamResponse(ctx, resp)
}

func buildUpstreamRequest(req *fasthttp.Request, ctx *fasthttp.RequestCtx, target string) {
	req.SetRequestURI(target)
	req.Header.SetMethod(string(ctx.Method()))

	if body := ctx.Request.Body(); len(body) > 0 {
		req.SetBody(body)
	}

	ctx.Request.Header.VisitAll(func(k, v []byte) {
		key := string(k)
		if key == "Host" || key == "Roblox-Id" {
			return
		}
		req.Header.SetBytesKV(k, v)
	})

	req.Header.Set("User-Agent", userAgent)
	req.SetHost("www.pekora.zip")
}

func doWithRetry(req *fasthttp.Request, resp *fasthttp.Response, attempts int) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = client.Do(req, resp); err == nil {
			return nil
		}
	}
	return err
}

func writeUpstreamResponse(ctx *fasthttp.RequestCtx, resp *fasthttp.Response) {
	resp.Header.VisitAll(func(k, v []byte) {
		if hopByHopHeaders[string(k)] {
			return
		}
		ctx.Response.Header.SetBytesKV(k, v)
	})
	ctx.SetStatusCode(resp.StatusCode())
	ctx.SetBody(resp.Body())
}

func handleStatic(ctx *fasthttp.RequestCtx, path string) {
	file := strings.TrimPrefix(path, "/")
	if file == "" {
		file = "index.html"
	}

	data, err := webFS.ReadFile("web/dist/" + file)
	if err != nil {
		if data, err = webFS.ReadFile("web/dist/index.html"); err != nil {
			ctx.SetStatusCode(fasthttp.StatusNotFound)
			return
		}
		file = "index.html"
	}

	ctx.SetContentType(contentType(file))
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(data)
}

func contentType(file string) string {
	switch {
	case strings.HasSuffix(file, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(file, ".js"):
		return "application/javascript"
	case strings.HasSuffix(file, ".css"):
		return "text/css"
	case strings.HasSuffix(file, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(file, ".json"):
		return "application/json"
	case strings.HasSuffix(file, ".wasm"):
		return "application/wasm"
	default:
		return "application/octet-stream"
	}
}