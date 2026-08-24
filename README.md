# reasoning-mesh

Software Engineering Experience Memory — multi-model LLM orchestration with knowledge accumulation from development outcomes.

Routes engineering tasks to specialized LLM adapters, retrieves relevant past experience via RAG, and continuously learns from CI outcomes.

ソフトウェアエンジニアリング経験メモリ — 開発成果から知識を蓄積するマルチモデル LLM オーケストレーションシステムです。

エンジニアリングタスクを専門の LLM アダプターへルーティングし、RAG で関連する過去の経験を取得しながら、CI の結果から継続的に学習します。

## Architecture / アーキテクチャ

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

## Prerequisites / 前提条件

| Service / サービス | Role / 役割 | Example endpoint |
| --- | --- | --- |
| Ollama with `qwen2.5:7b` + `ornith:9b` | LLM inference / LLM 推論 | `http://GPU_HOST:11434` |
| Qdrant | Vector store / ベクトルストア | `http://QDRANT_HOST:6333` |
| Embedding service (e5-mistral, port 9092) | Text embeddings / テキスト埋め込み | `http://EMBED_HOST:9092` |

## Setup / セットアップ

```bash
git clone https://github.com/flipslidersand/reasoning-mesh
cd reasoning-mesh
go build ./...
cp config/config.yaml.example config/config.yaml
# Edit config/config.yaml with your endpoints
```

## Running / 起動

```bash
LLMO_ORCH_TOKEN=your-secret ./orch --config config/config.yaml
curl http://localhost:8765/v1/health
```

## CLI usage / CLI の使い方

```bash
go build -o llmo ./cmd/llmo
./llmo ask --server http://localhost:8765 --task "Fix the nil pointer panic in user.go:42"
./llmo ingest --commit HEAD
./llmo eval --models qwen2.5:7b,ornith:9b --conditions all
./llmo bench results/
```

## Eval conditions / 評価条件

| Condition / 条件 | Description / 説明 |
| --- | --- |
| `no-rag` | Model only, no retrieval / モデルのみ、検索なし |
| `cosine` | Top-K by cosine similarity / コサイン類似度による Top-K |
| `score` | Composite scorer (recency + usage + task-type boost) / 複合スコアリング |
| `compressed` | Score retrieval + context compression / スコア検索 + コンテキスト圧縮 |

## CI — automatic knowledge ingest / 自動ナレッジ取り込み

Set in your GitHub repository / GitHub リポジトリに設定:
- `LLMO_TRIGGER_URL` — public URL of the orch server / orch サーバーの公開 URL
- `LLMO_TRIGGER_TOKEN` — token for `POST /v1/trigger`

On every push to `main`, diffs and CI logs are ingested into Qdrant automatically.
`main` へのプッシュのたびに、差分と CI ログが自動的に Qdrant へ取り込まれます。

## Development / 開発

```bash
go test ./...
golangci-lint run
go build ./...
```

## License

MIT
