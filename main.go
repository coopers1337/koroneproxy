package main

import (
	"embed"
	"log"
	"os"
	"strings"
	"time"

	"github.com/valyala/fasthttp"
)

//go:embed web/dist
var webFS embed.FS

const (
	userAgent  = "KoroneProxy/1"
	targetHost = "www.pekora.zip"
	baseTarget = "https://" + targetHost
	apiPrefix  = "/apisite/"
	healthPath = "/healthz"
	timeout    = 12 * time.Second
	maxRetries = 1
)

// Hop-by-hop + headers that would break the upstream request.
var stripReq = map[string]struct{}{
	"Host": {}, "Connection": {}, "Keep-Alive": {},
	"Proxy-Authenticate": {}, "Proxy-Authorization": {},
	"Te": {}, "Trailer": {}, "Transfer-Encoding": {}, "Upgrade": {},
	"User-Agent": {}, "Roblox-Id": {},
}

// Hop-by-hop + Content-Encoding (body is decompressed before write).
var stripResp = map[string]struct{}{
	"Connection": {}, "Keep-Alive": {},
	"Proxy-Authenticate": {}, "Proxy-Authorization": {},
	"Te": {}, "Trailer": {}, "Transfer-Encoding": {}, "Upgrade": {},
	"Content-Encoding": {},
}

var upstream = &fasthttp.Client{
	ReadTimeout:            timeout,
	WriteTimeout:           timeout,
	MaxIdleConnDuration:    90 * time.Second,
	MaxConnsPerHost:        256,
	NoDefaultUserAgentHeader: true,
	DisablePathNormalizing: true,
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	s := &fasthttp.Server{
		Handler:            router,
		ReadTimeout:        timeout,
		WriteTimeout:       timeout,
		IdleTimeout:        60 * time.Second,
		ReduceMemoryUsage:  true,
		MaxRequestBodySize: 4 << 20, // 4 MiB
	}

	log.Printf("koroneproxy :%s → %s", port, targetHost)
	if err := s.ListenAndServe(":" + port); err != nil {
		log.Fatal(err)
	}
}

func router(ctx *fasthttp.RequestCtx) {
	path := string(ctx.Path())
	switch {
	case path == healthPath:
		ctx.SetContentType("application/json")
		ctx.SetBodyString(`{"status":"ok"}`)
	case strings.HasPrefix(path, apiPrefix):
		proxy(ctx, path)
	default:
		static(ctx, path)
	}
}

func proxy(ctx *fasthttp.RequestCtx, path string) {
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	uri := baseTarget + path
	if q := ctx.QueryArgs().QueryString(); len(q) > 0 {
		uri += "?" + string(q)
	}

	req.SetRequestURI(uri)
	req.Header.SetMethodBytes(ctx.Method())
	req.SetHost(targetHost)
	req.Header.Set("User-Agent", userAgent)

	if b := ctx.Request.Body(); len(b) > 0 {
		req.SetBody(b)
	}

	ctx.Request.Header.VisitAll(func(k, v []byte) {
		if _, skip := stripReq[string(k)]; skip {
			return
		}
		req.Header.SetBytesKV(k, v)
	})

	var err error
	for i := 0; i <= maxRetries; i++ {
		if i > 0 {
			resp.Reset()
			time.Sleep(40 * time.Millisecond)
		}
		err = upstream.DoTimeout(req, resp, timeout)
		if err == nil || err == fasthttp.ErrTimeout {
			break
		}
	}
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusBadGateway)
		ctx.SetBodyString("upstream unavailable")
		return
	}

	resp.Header.VisitAll(func(k, v []byte) {
		if _, skip := stripResp[string(k)]; skip {
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

func static(ctx *fasthttp.RequestCtx, path string) {
	name := strings.TrimPrefix(path, "/")
	if name == "" || strings.Contains(name, "..") {
		name = "index.html"
	}

	data, err := webFS.ReadFile("web/dist/" + name)
	if err != nil {
		data, err = webFS.ReadFile("web/dist/index.html")
		if err != nil {
			ctx.SetStatusCode(fasthttp.StatusNotFound)
			return
		}
		name = "index.html"
	}

	if name == "index.html" {
		ctx.Response.Header.Set("Cache-Control", "no-cache")
	} else {
		ctx.Response.Header.Set("Cache-Control", "public, max-age=86400, immutable")
	}
	ctx.SetContentType(mime(name))
	ctx.SetBody(data)
}

func mime(name string) string {
	switch {
	case strings.HasSuffix(name, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "application/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	case strings.HasSuffix(name, ".ico"):
		return "image/x-icon"
	default:
		return "application/octet-stream"
	}
}
