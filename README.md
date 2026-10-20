# URL Shortener with Analytics

A small HTTP service that turns long HTTP or HTTPS addresses into short links. It supports custom codes, redirects, visit counts, link statistics, listing, and deletion. Links and visit analytics are saved to a JSON file so they survive service and container restarts.

- **Student:** Campan Alexandru, group and student ID to be completed
- **Project:** 1 — URL Shortener with Analytics
- **Language:** Go

## Features

- Create a link with an optional custom code (`POST /links`).
- Redirect from `/{code}` and record the visit.
- List links (`GET /links`) and inspect analytics (`GET /links/{code}/stats`).
- Delete a link (`DELETE /links/{code}`).
- Common service routes: `/`, `/health`, `/version`, and `POST /reset`.
- Validate destination URLs and custom codes; return JSON errors for invalid or missing resources.

The service keeps an in-memory map for fast lookups and writes each change to a JSON snapshot using an atomic file replacement. Local runs use `data/links.json`; the Docker image uses `/data/links.json`. Mount a Docker volume at `/data` to retain data when a container is recreated. Each redirect is recorded durably before the 302 response is sent, which favors accurate analytics over the fastest possible redirect.

## Run locally

Requires Go 1.27 or newer.

```sh
go run ./cmd/app
```

The server listens on `http://localhost:8080`. To check it, open that address or run:

```sh
curl http://localhost:8080/health
```

To print build information:

```sh
go run ./cmd/app --version
```

## Create and use a short link

```sh
curl -i -X POST http://localhost:8080/links \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://go.dev/doc/effective_go","code":"gofmt"}'

curl -i http://localhost:8080/gofmt
curl http://localhost:8080/links/gofmt/stats
```

Omit `code` to have the service generate one. Custom codes must contain 4–10 letters, digits, hyphens, or underscores. Destinations must be absolute `http` or `https` URLs.

## Run the published container

```sh
docker run --rm -p 8080:8080 -v atad-url-shortener-data:/data ghcr.io/alexandrucampan/atad-url-shortener:latest
```

## Project details to complete

Before submission, fill in the real group and student ID in `atad.json` and update the student line above. The project number, title, language, and container command are already filled in `atad.json`. The group and student ID still need to be added when available.

The architecture note is in [`docs/architecture.md`](docs/architecture.md). The final report belongs at `docs/report.docx`.
