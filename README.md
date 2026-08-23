# reasoning-mesh

Software Engineering Experience Memory — multi-model LLM orchestration with knowledge accumulation from development outcomes.

Routes engineering tasks to specialized LLM adapters, retrieves relevant past experience via RAG, and continuously learns from CI outcomes.

## Architecture

```text
llmo CLI / CI trigger
       │
       ▼
  orch server (POST /v1/infer)
       │
       ├─ router → classifies task type (debugging/testing/architecture/implementation)
       │
       ├─ knowledge retriever ← Qdrant (rm_knowledge)
       │      └─ embedder (e5-mistral)
       │
       └─ model adapters
              ├─ qwen2.5:7b  — debugging / testing / default
              ├─ ornith:9b   — implementation
              └─ claude      — architecture (optional, needs ANTHROPIC_API_KEY)
```

Knowledge accumulates automatically: every merged commit's diff + CI log is ingested via `POST /v1/trigger` and stored in Qdrant. Future queries retrieve similar past experience to improve response quality.

## Prerequisites

| Service | Role | Example endpoint |
| --- | --- | --- |
| [Ollama](https://ollama.ai) with `qwen2.5:7b` + `ornith:9b` | LLM inference | `http://GPU_HOST:11434` |
| [Qdrant](https://qdrant.tech) | Vector store | `http://QDRANT_HOST:6333` |
| Embedding service (e5-mistral, port 9092) | Text embeddings | `http://EMBED_HOST:9092` |

## Setup

```bash
git clone https://github.com/flipslidersand/reasoning-mesh
cd reasoning-mesh
go build ./...

cp config/config.yaml.example config/config.yaml
# Edit config/config.yaml with your endpoints
```

## Running the server

```bash
# Start orch server (default port 8765)
LLMO_ORCH_TOKEN=your-secret ./orch --config config/config.yaml

# Health check
curl http://localhost:8765/v1/health
```

`LLMO_ORCH_TOKEN` protects all endpoints except `/v1/health`. Omitting it leaves endpoints unprotected (development only).

## CLI usage

```bash
# Build the CLI
go build -o llmo ./cmd/llmo

# Ask a question (routes to appropriate model via orch server)
./llmo ask --server http://localhost:8765 --task "Fix the nil pointer panic in user.go:42"

# Ingest a git commit into the knowledge base
./llmo ingest --commit HEAD

# Run the eval harness (21 cases × models × conditions)
./llmo eval --models qwen2.5:7b,ornith:9b --conditions all --cooldown 5

# Show longitudinal benchmark across result files
./llmo bench results/

# Directly insert a knowledge entry (no orch server needed)
./llmo ingest-local --content "..." --task-type debugging
```

## Eval conditions

| Condition | Description |
| --- | --- |
| `no-rag` | Model only, no retrieval |
| `cosine` | Top-K by cosine similarity |
| `score` | Composite scorer (recency + usage + task-type boost) |
| `compressed` | Score retrieval + context compression |

## CI — automatic knowledge ingest

Set the following in your GitHub repository:

| Key | Type | Value |
| --- | --- | --- |
| `LLMO_TRIGGER_URL` | Variable | Public URL of the orch server |
| `LLMO_TRIGGER_TOKEN` | Secret | Token for `POST /v1/trigger` |

On every push to `main`, the workflow diffs the commit, bundles the CI test log, and posts to `/v1/trigger`. The server extracts knowledge chunks and upserts them to Qdrant. Set `LLMO_TRIGGER_URL` to skip ingest on forks/dev branches.

## Security

- **`LLMO_ORCH_TOKEN`** — bearer token for all server endpoints. Set as an environment variable; never commit to config files.
- **`LLMO_TRIGGER_TOKEN`** — separate token scoped to CI's `POST /v1/trigger` only.
- Config files (`config/config.yaml`) are `.gitignore`d. Use `config/config.yaml.example` as a template with no real secrets.

## Development

```bash
# Run all tests
go test ./...

# Lint
golangci-lint run

# Build
go build ./...
```

Test coverage by package: `qdrant` 60%+, `router` 50%+, `config` 90%+.

## Directory structure

```text
cmd/
  llmo/       CLI (ask / ingest / eval / bench / ingest-local)
  orch/       HTTP server
internal/
  config/     Config loading + validation
  eval/       Eval harness, cases, result types
  knowledge/  Embedder, retriever, structurizer, score updater
  ollama/     Ollama HTTP client
  qdrant/     Qdrant HTTP client (upsert / search / EnsureCollection)
  router/     Task-type classifier + model adapter dispatch
  server/     HTTP handlers (/v1/infer /v1/trigger /v1/feedback)
  telemetry/  OpenTelemetry setup
cases/        YAML eval test cases
config/       config.yaml.example
results/      Eval JSON outputs (gitignored)
```
