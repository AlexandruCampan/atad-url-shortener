# Architecture

## Shape

The executable starts in `cmd/app`. It creates a standard-library `http.ServeMux`, registers the shared service routes (`/`, `/health`, `/version`, and `POST /reset`), then registers the URL-shortener routes from `internal/shortener`. The HTTP server listens on port 8080 and shuts down cleanly when it receives an interrupt or termination signal.

For link creation, the handler decodes and validates the JSON request, obtains the store lock, chooses or checks a code, and stores the new link. For a redirect, it looks up the code and updates its visit count and last-visit time while holding the lock, then sends an HTTP 302 response to the destination. Listing, statistics, and deletion use the same store. Errors are returned as JSON.

## Data

`internal/shortener.Store` holds a map keyed by short code and an integer used to assign IDs. Each `Link` contains its ID, code, destination URL, creation time, visit count, and optional last-visit time. A mutex protects access to the map and its link records. All data is in process memory; nothing survives a restart. `POST /reset` clears the store and resets the ID counter.

## Three decisions

1. **In-memory map for the first implementation.** This keeps the initial service small and makes the route and analytics behavior easy to demonstrate. A database would preserve data across restarts, but adds schema, connection, and deployment work. Persistence remains future work.
2. **Standard-library HTTP routing.** Go's `http.ServeMux` supports method and path patterns used here, so the project has no router dependency to configure or update. A third-party router could offer additional conveniences, but is not needed for the current API.
3. **Mutex-protected shared store.** The server handles requests concurrently, so the map and mutable visit records need synchronization. A single mutex is straightforward for this small workload. A database transaction or finer-grained locking may suit a larger service, but is unnecessary at this stage.

## Not built yet

- Persistent storage; links and analytics disappear on restart.
- A user interface for managing links beyond the small landing page and HTTP API.
- Expiration, authentication, rate limiting, and richer analytics or filtering.
- Deployment beyond the CI-built container image.

This note describes the current implementation and will need updating as these features are developed.
