# Architecture

## Shape

The executable starts in `cmd/app`. It creates a standard-library `http.ServeMux`, registers the shared service routes (`/`, `/health`, `/version`, and `POST /reset`), then registers the URL-shortener routes from `internal/shortener`. The HTTP server listens on port 8080 and shuts down cleanly when it receives an interrupt or termination signal.

For link creation, the handler decodes and validates the JSON request, obtains the store lock, chooses or checks a code, and stores the new link. For a redirect, it looks up the code and updates its visit count and last-visit time while holding the lock. The updated snapshot is flushed to disk before the service sends an HTTP 302 response. Listing, statistics, and deletion use the same store. Errors are returned as JSON.

## Data

`internal/shortener.Store` holds a map keyed by short code and an integer used to assign IDs. Each `Link` contains its ID, code, destination URL, creation time, visit count, and optional last-visit time. A mutex protects access to the map and its link records. After each mutation, the service serializes the complete state to a temporary file, flushes it, and atomically renames it to the configured data file. Local runs default to `data/links.json`; Docker sets `ATAD_DATA_FILE=/data/links.json` and exposes `/data` as a volume. `POST /reset` clears the store, persists the empty state, and resets the ID counter. Visit writes happen before redirects, favoring durable analytics over redirect latency.

## Three decisions

1. **JSON snapshots instead of a database.** A complete snapshot keeps the project dependency-free and makes persistence easy to inspect. SQLite would offer transactions and better write performance for larger data sets, but adds a database dependency and schema work. Snapshot writes happen synchronously, so each redirect pays a disk-write cost.
2. **Standard-library HTTP routing.** Go's `http.ServeMux` supports method and path patterns used here, so the project has no router dependency to configure or update. A third-party router could offer additional conveniences, but is not needed for the current API.
3. **Mutex-protected shared store.** The server handles requests concurrently, so the map and mutable visit records need synchronization. A single mutex is straightforward for this small workload. A database transaction or finer-grained locking may suit a larger service, but is unnecessary at this stage.

## Not built yet

- A user interface for managing links beyond the small landing page and HTTP API.
- Expiration, authentication, rate limiting, and richer analytics or filtering.
- Deployment beyond the CI-built container image.
- Faster analytics writes, such as a durable append-only event log or a database, if redirect latency becomes a bottleneck.

This note describes the current implementation and will need updating as these features are developed.
