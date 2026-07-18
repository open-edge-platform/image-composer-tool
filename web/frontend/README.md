# Image Composer Tool - Web Frontend

This directory contains the entire frontend layer of the Image Composer Tool. It is built as a Single Page Application (SPA) using React, and is served and bundled using Vite.

## Goal & Architecture Context
The core objective is to build a web interface for the Image Composer Tool. The architecture ensures that the frontend is a thin layer, pushing all business logic (like AI generation, template validation, and session management) to the Go backend.

1. **Framework & Bundler:** The frontend is a React application built with Vite.
2. **Backend Communication:** The frontend communicates with a Go backend (running on port `8080`). During development, Vite (running on port `5173`) uses its `server.proxy` feature in `vite.config.js` to automatically forward all requests starting with `/api/*` to the Go backend, sidestepping CORS issues.
3. **SSE Streaming (AI Generation):** Template generation utilizes Server-Sent Events (SSE). The frontend uses the native `EventSource` API to listen to the `GET /api/v1/ai/stream` endpoint, managed by the `useSSE.js` hook. No WebSockets are used.
4. **Styling:** We use **Vanilla CSS with CSS modules** (`.module.css`). There is no Tailwind or external component library. All design tokens (colors, spacing, typography) are defined as CSS variables in `index.css`.
5. **State Management:** State is managed via React Context and hooks (`useState`, `useReducer`, `useRef`). For instance, `SessionContext.jsx` manages active conversations, and `useChat.js` manages individual chat interactions.

## Technology Decisions
- **React + Vite:** Used over CRA for instant HMR, native ES module serving, and built-in API proxying capabilities.
- **React Router v7:** Handles client-side routing across the 4 distinct views (Chat, Editor, Template Library, Build Dashboard) and supports lazy loading.
- **Vanilla CSS:** Fulfills the requirement for maximum flexibility without build-tool dependencies like Tailwind. Uses CSS custom properties for theming.
- **CodeMirror 6 (Phase 4):** Chosen over Monaco for the YAML editor due to being modular, tree-shakable, and lightweight.
- **Context + useReducer:** A minimal native state management solution, avoiding overhead from Redux/Zustand since the state surface area is small.

## Directory & File Structure

```text
image-composer-tool/
├── web/
│   └── frontend/                 # React SPA root
│       ├── index.html            # Vite entry point
│       ├── package.json
│       ├── vite.config.js
│       └── src/
│           ├── main.jsx          # React DOM root, router setup
│           ├── App.jsx           # Top-level layout (sidebar + content)
│           ├── App.module.css
│           ├── index.css         # Global styles, CSS custom properties
│           │
│           ├── api/              # API client layer (Thin fetch wrappers)
│           │   ├── client.js     # Base fetch wrapper, error handling
│           │   ├── ai.js         # AI endpoints (query, search, stream)
│           │   ├── sessions.js   # Session endpoints
│           │   ├── engine.js     # Health + stats endpoints
│           │   ├── templates.js  # Template CRUD endpoints (Phase 4)
│           │   └── builds.js     # Build endpoints (Phase 5)
│           │
│           ├── hooks/            # Custom React hooks
│           │   ├── SessionContext.jsx # Session provider and context
│           │   ├── useChat.js    # Chat state management and submission logic
│           │   ├── useSSE.js     # EventSource hook for streaming
│           │   ├── useHealth.js  # Engine health polling
│           │   └── useEngineStats.js # Engine stats tracking
│           │
│           ├── components/       # Shared UI components
│           │   ├── Sidebar/
│           │   ├── StatusIndicator/
│           │   ├── StreamingYaml/
│           │   ├── ChatInput/
│           │   ├── MessageBubble/
│           │   ├── SearchResultCard/
│           │   └── ErrorBanner/
│           │
│           └── views/            # Route-level page components
│               ├── ChatView/            # Phase 2
│               ├── EditorView/          # Phase 4
│               ├── TemplateLibraryView/ # Phase 4
│               └── BuildDashboardView/  # Phase 5
```

## Architecture Patterns

### API Client Layer (`src/api/`)
A thin wrapper around `fetch()` that prepends the base URL, handles JSON serialization, maps API error responses into typed JavaScript errors, and provides an SSE helper (`api/ai.js`) that wraps `EventSource` with typed event callbacks (`search_results`, `generation_start`, `token`, `generation_complete`, `complete`, `error`).

### Component Hierarchy
The top-level `App.jsx` provides the overarching layout including the `Sidebar` and `StatusIndicator`. React Router swaps out the main content view (e.g., `ChatView`). The `ChatView` orchestrates smaller components like `ChatInput`, `MessageBubble`, `SearchResultCard`, and `StreamingYaml`.

### State Flow for a Streaming Query
1. User types query in `ChatInput` and submits.
2. `useChat` adds the user message to the state and initiates an SSE stream via `api/ai.js`.
3. The UI receives `search_results` and displays them as cards.
4. As `token` events arrive, `StreamingYaml` appends them to its buffer in real-time.
5. On `generation_complete`, the final YAML is stored.
6. On `complete`, the assistant message is finalized in the chat history.

## Design System
The application utilizes a premium dark theme defined in `src/index.css`. This provides excellent contrast for syntax-highlighted YAML and matches developer tooling conventions. CSS custom properties (`--color-bg-primary`, `--font-sans`, `--space-md`) enforce a consistent design token system across all CSS modules.

## Phase-Aligned Delivery

- **Phase 2 (Completed):** Chat UI + Streaming. Included full project scaffolding, design system, API client layer, SSE hooks, layout, and the core Chat View with streaming YAML capabilities.
- **Phase 3 (Completed):** Sessions + Multi-turn. Introduced `SessionContext`, wired `session_id` into queries, added conversation history tracking, and created the "New Conversation" flow.
- **Phase 4 (Future):** Editor + Template Library. Will introduce CodeMirror 6 for YAML editing, real-time validation, and a template browser with full CRUD actions.
- **Phase 5 (Future):** Build Dashboard. Will introduce build triggering, log streaming via SSE, and status tracking.

## Development Workflow

To run the application in development:

```bash
# Terminal 1 (Backend):
cd image-composer-tool
./image-composer-tool serve

# Terminal 2 (Frontend):
cd image-composer-tool/web/frontend
npm run dev
```

The Vite dev server on port `5173` will automatically proxy all `/api/*` requests to the Go backend on `localhost:8080`. Hot module replacement works seamlessly for all React components and CSS modules.
