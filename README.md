# KoroneProxy

Tiny fasthttp reverse proxy for pekora.zip `/apisite/*` so Roblox Studio can call the API without trustcheck blocking.

## Deploy (Render free)

1. Push this repo to GitHub.
2. New Web Service → connect repo → Render detects `render.yaml`.
3. Deploy. Health check hits `/healthz`.

Binary is static (`netgo` + `-s -w`), ~5 MB. Free tier sleeps after idle; cold start is a few seconds.

## Optional free CDN

Put **Cloudflare** (free) in front of the Render URL:

- Orange-cloud the CNAME
- SSL/TLS → Full
- Caching level → Standard (API paths stay dynamic; static UI caches)

That cuts latency for the URL converter page and gives a nicer domain without paying Render for custom domains on free.

## Local

```bash
go run .
# open http://localhost:8080
```

Proxy path: `GET/POST /apisite/...` → `https://www.pekora.zip/apisite/...`

## Layout

```
main.go          # single-file proxy + static
web/dist/        # embedded UI (shadcn-style dark)
go.mod
render.yaml
```
