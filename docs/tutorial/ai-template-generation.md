# AI-Powered Template Generation (RAG)

Generate OS image templates from natural language descriptions using
Retrieval-Augmented Generation (RAG). The AI feature searches existing
templates semantically, then uses an LLM to generate a new template grounded
in real, working examples.

> **Phase 1** - This guide covers the current implementation: core RAG
> with basic CLI (semantic search, template generation, embedding cache).
> See the [ADR](../architecture/adr-template-enriched-rag.md) for the full
> roadmap (query classification, conversational refinement, agentic
> validation).

## Table of Contents

- [AI-Powered Template Generation (RAG)](#ai-powered-template-generation-rag)
  - [Table of Contents](#table-of-contents)
  - [Prerequisites](#prerequisites)
    - [Install Ollama (recommended)](#install-ollama-recommended)
  - [Quick Start (Ollama - local, free)](#quick-start-ollama---local-free)
  - [Quick Start (OpenAI - cloud)](#quick-start-openai---cloud)
  - [CLI Reference](#cli-reference)
    - [Generate a Template](#generate-a-template)
    - [Search Only](#search-only)
    - [Save to File](#save-to-file)
    - [Cache Management](#cache-management)
    - [All Flags](#all-flags)
  - [Configuration](#configuration)
    - [Zero Configuration (Ollama)](#zero-configuration-ollama)
    - [Switching to OpenAI](#switching-to-openai)
    - [Full Configuration Reference](#full-configuration-reference)
  - [How It Works](#how-it-works)
  - [Enriching Templates with Metadata](#enriching-templates-with-metadata)
  - [Troubleshooting](#troubleshooting)
  - [Related Documentation](#related-documentation)

---

## Prerequisites

| Requirement | Details |
|-------------|---------|
| **ict** binary | Built via `earthly +build` or `go build ./cmd/image-composer-tool` |
| **AI provider** (one of) | [Ollama](https://ollama.com) (local) **or** an [OpenAI](https://platform.openai.com) API key |
| **Template files** | At least a few `image-templates/*.yml` files to serve as the RAG knowledge base |

### Install Ollama (recommended)

Ollama runs models locally - no API keys, no cloud costs.

```bash
# Install Ollama (Linux)
curl -fsSL https://ollama.com/install.sh | sh

# Pull the required models
ollama pull nomic-embed-text   # embedding model (768 dimensions)
ollama pull llama3.1:8b        # default chat/generation model

# Verify the server is running
ollama list
```

> **Tip:** Alternative embedding models are supported:
> `mxbai-embed-large` (1024 dims) and `all-minilm` (384 dims). Change the
> model in `image-composer-tool.yml` if needed.

---

## Quick Start (Ollama - local, free)

With Ollama running, no configuration is needed:

```bash
# 1. Make sure Ollama is serving (default http://localhost:11434)
ollama serve &

# 2. Generate a template from a natural language description
./image-composer-tool ai "create a minimal edge image for ubuntu with SSH"

# 3. Search for relevant templates without generating
./image-composer-tool ai --search-only "cloud image with monitoring"
```

## Quick Start (OpenAI - cloud)

```bash
# 1. Set your API key
export OPENAI_API_KEY="sk-..."

# 2. Generate using OpenAI
./image-composer-tool ai --provider openai "create a minimal elxr image for IoT"
```

---

## CLI Reference

```
image-composer-tool ai [query] [flags]
```

### Generate a Template

```bash
./image-composer-tool ai "create a minimal edge image for elxr with docker support"
```

The command will:

1. Index all templates in `image-templates/` (with embedding cache)
2. Perform semantic search to find the most relevant templates
3. Show the top reference templates and their similarity scores
4. Generate a new YAML template grounded in those examples

### Search Only

Find relevant templates without invoking the LLM:

```bash
./image-composer-tool ai --search-only "cloud deployment with monitoring"
```

Output shows each matching template with a score breakdown:

```
Found 5 matching templates:

1. elxr-cloud-amd64.yml
   Score: 0.87 (semantic: 0.92, keyword: 0.75, package: 0.60)
   Description: Cloud-ready eLxr image for VM deployment
   Distribution: elxr12, Architecture: x86_64, Type: raw
```

### Save to File

```bash
# Save to image-templates/my-custom-image.yml
./image-composer-tool ai "create a minimal edge image" --output my-custom-image

# Save to a specific path
./image-composer-tool ai "create an edge image" --output /tmp/my-image.yml
```

If the output filename matches one of the reference templates returned by
the current search results, you will be prompted before overwriting.

### Cache Management

Embeddings are cached to avoid recomputation on each run. The cache
automatically invalidates when a template's content changes (SHA256 hash).

```bash
# Show cache statistics (entries, size, model, dimensions)
./image-composer-tool ai --cache-stats

# Clear the embedding cache (forces re-indexing on next run)
./image-composer-tool ai --clear-cache
```

### All Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--provider` | `ollama` | AI provider: `ollama` or `openai` |
| `--templates-dir` | `./image-templates` | Directory containing template YAML files |
| `--search-only` | `false` | Only search, don't generate |
| `--output` | _(none)_ | Save generated template (name or path) |
| `--cache-stats` | `false` | Show cache statistics |
| `--clear-cache` | `false` | Clear the embedding cache |

---

## Configuration

### Zero Configuration (Ollama)

If Ollama is running on `localhost:11434` with `nomic-embed-text` and
`llama3.1:8b` pulled, everything works out of the box - no config file changes
required.

### Switching to OpenAI

The AI command currently selects the provider via CLI flags, not
`image-composer-tool.yml`.

Use `--provider openai` when running `image-composer-tool ai`:

```bash
./image-composer-tool ai --provider openai "minimal Ubuntu server image for cloud VMs"
```

You also need an API key:

```bash
export OPENAI_API_KEY="sk-..."
```

The config snippet below shows the global config file schema for reference:

```yaml
ai:
  provider: openai
```

### Full Configuration Reference

All settings are optional. Defaults are shown below - only override what you
need to change in `image-composer-tool.yml`:

```yaml
ai:
  provider: ollama                # "ollama" or "openai"
  templates_dir: ./image-templates

  ollama:
    base_url: http://localhost:11434
    embedding_model: nomic-embed-text   # 768 dims
    chat_model: llama3.1:8b
    timeout: "120s"                    # request timeout

  openai:
    embedding_model: text-embedding-3-small
    chat_model: gpt-4o-mini
    timeout: "60s"                     # request timeout

  cache:
    enabled: true
    dir: ./.ai-cache

  # Advanced - rarely need to change
  scoring:
    semantic_weight: 0.70   # embedding similarity weight
    keyword_weight: 0.20    # keyword overlap weight
    package_weight: 0.10    # package name matching weight
```

| Environment Variable | Description |
|----------------------|-------------|
| `OPENAI_API_KEY` | Required when `provider: openai` |

---

## How It Works

```
User query ──► Index templates ──► Semantic search ──► Build LLM context ──► Generate YAML
                  │                     │
                  ▼                     ▼
             Embedding cache      Hybrid scoring
             (SHA256 hash)    (semantic + keyword + package)
```

1. **Indexing** - On first run (or when templates change), each template in
   `image-templates/` is parsed and converted to a searchable text
   representation. An embedding vector is generated via the configured
   provider and cached locally.

2. **Search** - The user query is embedded and compared against all template
   vectors using cosine similarity. A hybrid score combines:
   - **Semantic similarity** (70%) - how closely the meaning matches
   - **Keyword overlap** (20%) - exact term matches
   - **Package matching** (10%) - package name overlap

3. **Generation** - The top-scoring templates are included as context for the
   LLM, which generates a new YAML template grounded in real, working
   examples.

---

## Enriching Templates with Metadata

Templates work without metadata, but adding an optional `metadata` section
improves search accuracy:

```yaml
metadata:
  description: "Cloud-ready eLxr image for VM deployment on AWS, Azure, GCP"
  use_cases:
    - cloud-deployment
  keywords:
    - cloud
    - cloud-init
    - aws
    - azure

image:
  name: elxr-cloud-amd64
  # ... rest of template
```

All metadata fields are optional. Templates without metadata are still
indexed using their filename, distribution, architecture, image type, and
package lists.

---

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `failed to create AI engine` | Ollama not running | Run `ollama serve` |
| `connection refused :11434` | Ollama server down | Start Ollama: `ollama serve` |
| Embeddings fail | Model not pulled | `ollama pull nomic-embed-text` |
| Chat generation fails | Chat model not pulled | `ollama pull llama3.1:8b` |
| Poor search results | Stale cache | `./image-composer-tool ai --clear-cache` |
| OpenAI auth error | Missing API key | `export OPENAI_API_KEY="sk-..."` |
| Slow first run | Building embedding cache | Normal - subsequent runs use cache |
| `No matching templates found` | Empty templates dir | Check `--templates-dir` points to templates |

---

## Related Documentation

- [ADR: Template-Enriched RAG](../architecture/adr-template-enriched-rag.md) -
  Full architecture decision record, design details, and roadmap
- [Usage Guide](usage-guide.md) - General image-composer-tool usage
- [Image Templates](../architecture/image-composer-tool-templates.md) -
  Template format specification
- [CLI Specification](../architecture/image-composer-tool-cli-specification.md) -
  Complete command reference
