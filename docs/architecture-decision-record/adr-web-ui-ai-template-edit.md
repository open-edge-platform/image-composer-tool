<!--
SPDX-FileCopyrightText: (C) 2026 Intel Corporation
SPDX-License-Identifier: Apache-2.0
-->

# ADR: Ask AI to Edit a Template in the Web UI

**Status**: Proposed (for team discussion)
**Date**: 2026-11-22
**Authors**: ICT Team
**Technical Area**: Web UI / AI
**Depends on**: [adr-web-ui-template-authoring.md](adr-web-ui-template-authoring.md),
[adr-template-enriched-rag.md](adr-template-enriched-rag.md)
**Related**: [adr-nl-package-selection.md](adr-nl-package-selection.md)

---

## Summary

In Author mode, the user types what they want, for example *"add docker,
remove snapd, make the disk 32 GiB"*. The AI suggests edits. The UI checks
each one and shows it in the **Changes** rail, where the user keeps or undoes
it. Nothing is saved or built until the user says so.

The AI runs on **GNAI**, Intel's OpenAI-compatible service. Any
OpenAI-compatible service works.

---

## Why

The CLI can already write a new template from a description
(`image-composer-tool ai`). Template owners also want to **change** an
existing template in plain words, in the UI they already use, without
looking up package names.

---

## Decision

### 1. The AI returns edits, not a new file

The server asks the model for a list of edits in a fixed JSON format:

| Edit | Example |
|---|---|
| Add a package | `docker.io` |
| Remove a package | `snapd` |
| Pin or unpin a version | `linux-image-7.3-intel` to `261014t080608z-resolute1` |
| Set the disk size | `32GiB` |
| Set the image name or hostname | `nvl-docker` |

The UI applies them the same way as hand edits, so comments and layout are
kept. Anything else is out of scope for phase 1. The AI says so in a short
note instead of guessing.

### 2. Every edit is checked before it is applied

The browser checks the edits, because it knows which repositories the user has
checked and their priorities.

- Package names and versions must exist in a repository for the target. The
  check uses the same lookup as the Packages step, and picks the repository a
  build would install from. A package from a repository not yet checked turns
  that repository on, as picking a search result does.
- A name that isn't found is listed as **not found**, with up to three similar
  names to click (Ubuntu's `docker.io` for "docker"). It is never added quietly.
- Disk sizes go through the Disk step's checks.

This follows the rule in [adr-nl-package-selection.md](adr-nl-package-selection.md):
the AI never puts an unchecked package name in a template.

### 3. The user decides

- AI edits show in the Changes rail with an **AI** tag. Each one can be undone
  with ↺.
- The AI's note ("Removed snapd. docker.io comes from Ubuntu base.") is shown
  above the rail.
- Save, Save As and Compose work as they do today.

### 4. The server talks to the AI

- New endpoint: `POST /api/v1/ai/template-edits` with the template text and
  the request. It returns the edits, the note, and what the edits can't
  express. It checks only their format; the browser checks the rest.
- The browser never calls GNAI, so the token stays on the server and there is
  no cross-origin (CORS) problem.
- User passwords and password hashes are removed from the template before it
  is sent.

### 5. Settings

| Setting | Phase 1 |
|---|---|
| Turn it on | `serve --enable-ai`, or `ICT_WEBUI_AI=true` in Docker Compose. Needs `--enable-authoring`. Off by default. |
| What the UI sees | `GET /api/v1/session` returns `capabilities.ai` |
| Service URL | `ai.openai.base_url` in `image-composer-tool.yml`, or `OPENAI_BASE_URL` |
| Token | `OPENAI_API_KEY`, or a file named by `OPENAI_API_KEY_FILE`, read for every request |
| Chat model | `ai.openai.chat_model`, or `OPENAI_MODEL` |

Ask AI always uses the OpenAI-compatible API, whatever `ai.provider` says, so
Docker Compose can set everything with environment variables.

For GNAI:

```yaml
ai:
  provider: openai
  openai:
    base_url: https://gnai.intel.com/api/providers/openai/v1
    chat_model: gpt-5.4-mini
    embedding_model: text-embedding-3-large
```

Intel employees get a token at <https://gnai.intel.com/auth/oauth2/sso>. It
expires. Writing the new one to the token file is enough; no restart. When GNAI
rejects the token, the UI shows that link.

In Docker, the container must trust Intel's CA certificates to reach GNAI.

### 6. Phase 1 limits

- Author mode only. Customize can follow later, because it uses the same
  Packages and Disk steps.
- One request at a time. No chat history.
- One server-wide token. Per-user sign-in comes with roles.
- The Changes rail tags package changes **AI**. Disk and image name changes
  show only in Ask AI's summary.

---

## What we checked on GNAI (2026-11-22)

| Check | Result |
|---|---|
| `GET /v1/models` | 30 models, including `gpt-5.4-mini` and `gpt-5.5` |
| Chat with `gpt-5.4-mini` | Works |
| JSON in a fixed format (`response_format: json_schema`) | Works |
| Embeddings with `text-embedding-3-large` | Works, returns **1024** numbers per text |
| `text-embedding-3-small` (the CLI default) | Not offered (404) |
| `temperature` other than 1 on `gpt-5-mini` | Rejected, so we don't send it |

---

## Trade-offs

- **The template leaves the machine.** It goes to GNAI, inside Intel. Passwords
  are removed first. Other services need the same review.
- **Answers can vary.** The checks and the review step keep wrong edits out of
  the template, but a request may need rewording.
- **It needs a token,** and SSO tokens expire.

---

## Options we didn't pick

- **The AI rewrites the whole YAML:** hard to review, loses comments, and can
  drop sections without anyone noticing.
- **The browser calls GNAI:** exposes the token in the page and fails on CORS.
- **A chat panel:** more to build, and phase 1 needs one request at a time.

---

## Revision History

| Date | Change |
|---|---|
| 2026-11-22 | Initial proposal |
| 2026-11-22 | The browser checks the edits; similar names for a missing package; token file read per request |
