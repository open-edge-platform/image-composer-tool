# API Package Reference
NOTE: This is a temporary file to just get rough idea and reference about what the temporary structure of the project backend looks like. This is not a final source of truth reference document and may contain minor errors for now. Will be updated in future as project progresses-NB

This document serves as a reference for the `internal/api` package, which provides the HTTP web server layer for the Image Composer Tool. It explains the purpose of each file, how they coordinate to handle requests, and the current development status.

##  File Breakdown

The `internal/api` directory is designed to be a thin HTTP wrapper around the existing `internal/ai` business logic.

*   **`server.go`**: Contains the central `Server` struct. This struct holds all shared dependencies (like the RAG engine and configuration) that handlers need. It is responsible for starting and stopping the underlying Go HTTP server.
*   **`router.go`**: Uses Go 1.22's `http.ServeMux` to map incoming HTTP request URLs (e.g., `POST /api/v1/ai/query`) to the specific handler functions that should process them. It acts as the traffic controller.
*   **`middleware.go`**: Contains the `withCORS` function. This wraps the router to intercept all incoming requests and attach CORS (Cross-Origin Resource Sharing) headers, allowing frontend web applications (like a React app on `localhost:3000`) to communicate with the server securely.
*   **`helpers.go`**: Provides reusable utility functions for all handlers. `decodeJSON` and `respondJSON` standardize how JSON data is read and written. `respondError` ensures that all API errors follow the exact OpenAPI schema format.
*   **`handlers_health.go`**: Contains the logic for the `/api/v1/health` and `/api/v1/engine/stats` endpoints. These interact with the AI engine to report on system readiness and internal metrics (like cache size and provider status).
*   **`handlers_ai.go`**: Contains the core AI generation endpoints (`/api/v1/ai/query` and `/api/v1/ai/search`). These handlers parse the user's natural language request, call the underlying `rag.Engine.Generate()` or `rag.Engine.Search()` methods, and format the results.
*   **`handlers_templates.go`**: Contains the logic for interacting with local OS templates (`/api/v1/templates` and `/api/v1/templates/{name}`). It calls `template.ScanTemplates()` and `template.ParseTemplate()` to read `.yml` files from the filesystem.
*   **`server_test.go`**: The automated testing suite that validates the routing, CORS middleware, and JSON serialization of the API endpoints without needing to run the CLI.

*(Note: There is one related file outside this folder: `cmd/image-composer-tool/serve_cmd.go`. This is the CLI entry point that initializes the AI engine, creates the `Server`, and calls `Server.Start()`).*

---

## Request Flow & Coordination

When a user or web browser sends a request, the files coordinate in the following sequence:

1.  **Entry**: The request hits the Go `http.Server` configured in `server.go`.
2.  **Middleware**: The request passes through `middleware.go`, which handles preflight `OPTIONS` requests and adds CORS headers to the response.
3.  **Routing**: The request reaches the multiplexer in `router.go`. Based on the HTTP method and path (e.g., `POST /api/v1/ai/query`), the router forwards the request to the correct handler function (e.g., `handleQuery`).
4.  **Handling**: Inside the specific handler (e.g., `handlers_ai.go`):
    *   It uses `helpers.go` to parse the incoming JSON payload.
    *   It makes a direct Go library call to the `Server.engine` (the RAG engine). **No CLI commands or subprocesses are used.**
    *   It receives the generated YAML template from the engine.
    *   It uses `helpers.go` to format the final JSON response and send it back to the client.

---

## What is Done (Phase 1 and Phase 2)


The core scaffolding and foundational architecture have been completed. The following OpenAPI specifications are fully implemented and functional:

*   `GET /api/v1/health` (System health check)
*   `GET /api/v1/engine/stats` (AI engine status)
*   `GET /api/v1/templates` (List all OS templates)
*   `GET /api/v1/templates/{name}` (Retrieve a specific OS template)
*   `GET /api/v1/ai/search` (Semantic search over templates)
*   `POST /api/v1/ai/query` (Standard, non-streaming template generation)


**Fixes Applied:** The server `WriteTimeout` was increased from 60 seconds to 5 minutes to accommodate large local LLMs (like `llama3.1:8b`) that require longer generation times.

**Phase 2: Streaming Generation**
    *   Implemented `GET /api/v1/ai/stream` to use Server-Sent Events (SSE). This allows real-time streaming of generation tokens to the frontend UI.

---

## ⏳ What is Yet To Be Done (Future Phases)

The API is built in phases according to the Architecture Decision Record (ADR). The following features are scheduled for future implementation:


*   **Phase 3: Conversational Sessions**
    *   Implement Session Manager.
    *   Add endpoints: `POST /api/v1/sessions`, `GET /api/v1/sessions/{id}`, `DELETE /api/v1/sessions/{id}`.
    *   Upgrade the existing `/ai/query` and `/ai/stream` endpoints to accept and utilize a `session_id` for chat history context.
*   **Phase 4: Template CRUD Operations**
    *   Add endpoints to create, modify, validate, and delete template files over the API (`POST /api/v1/templates`, `PUT /api/v1/templates/{name}`, etc.).
*   **Phase 5: Remote Builds**
    *   Implement Build Manager.
    *   Add endpoints to trigger (`POST /api/v1/builds`), monitor (`GET /api/v1/builds/{id}`), and stream logs (`GET /api/v1/builds/{id}/logs`) for `sudo osbuild` processes.
*   **CLI Configuration Integration**
    *   Update `serve_cmd.go` to merge the global `image-composer-tool.yml` configuration (so that properties like `chat_model` set by the user are respected by the web server).
