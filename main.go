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
	port       = "8080"
	timeout    = 10 * time.Second
	retries    = 3
)

var client = &fasthttp.Client{
	ReadTimeout:              timeout,
	WriteTimeout:             timeout,
	MaxIdleConnDuration:      60 * time.Second,
	NoDefaultUserAgentHeader: true,
	DisablePathNormalizing:   true,
}

func main() {
	log.Printf("KoroneProxy listening on :%s", port)
	if err := fasthttp.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("ListenAndServe: %s", err)
	}
}

func handler(ctx *fasthttp.RequestCtx) {
	path := string(ctx.Path())

	if path == "/healthz" {
		ctx.SetStatusCode(fasthttp.StatusOK)
		ctx.SetContentType("application/json")
		ctx.SetBodyString(`{"status":"ok"}`)
		return
	}

	if !strings.HasPrefix(path, "/apisite/") {
		serveStatic(ctx, path)
		return
	}

	target := baseTarget + path
	if qs := string(ctx.QueryArgs().QueryString()); qs != "" {
		target += "?" + qs
	}

	proxy(ctx, target, 1)
}

func proxy(ctx *fasthttp.RequestCtx, target string, attempt int) {
	if attempt > retries {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString("Proxy failed to connect. Please try again.")
		return
	}

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(target)
	req.Header.SetMethod(string(ctx.Method()))
	req.SetBody(ctx.Request.Body())

	ctx.Request.Header.VisitAll(func(k, v []byte) {
		req.Header.SetBytesKV(k, v)
	})
	req.Header.Set("User-Agent", userAgent)
	req.Header.Del("Roblox-Id")

	if err := client.Do(req, resp); err != nil {
		proxy(ctx, target, attempt+1)
		return
	}

	ctx.SetStatusCode(resp.StatusCode())
	resp.Header.VisitAll(func(k, v []byte) {
		ctx.Response.Header.SetBytesKV(k, v)
	})
	ctx.SetBody(resp.Body())
}

func serveStatic(ctx *fasthttp.RequestCtx, path string) {
	file := strings.TrimPrefix(path, "/")
	if file == "" {
		file = "index.html"
	}

	data, err := webFS.ReadFile("web/dist/" + file)
	if err != nil {
		data, err = webFS.ReadFile("web/dist/index.html")
		if err != nil {
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
	default:
		return "application/octet-stream"
	}
}