---
title: "eval コマンドに単一モデルだけ渡すと、そのモデルが自分自身の回答を判定してしまう"
category: go
date: "2026-09-12"
---

## 症状

`go run ./cmd/llmo eval -models ornith:9b -conditions no-rag` のように
`-models` にモデルを1つだけ渡して実行すると、`docs/ornith-improvement.md` の
過去ベースライン（no-rag accuracy 0.54）より大幅に悪い accuracy（0.45）が出る。
一見「変更で退行した」ように見える。

## 原因

`internal/eval/runner.go` の `judgeAccuracy` は常に `r.models[0]` を
judge（採点者）として使う（`-models` フラグが未指定なら `router,knowledge`＝
`qwen2.5:7b,ornith:9b` になり `models[0]=qwen2.5:7b` が judge）。
`-models ornith:9b` のように1モデルだけ渡すと `models[0]` もそのモデル自身になり、
ornith:9b が自分の回答を自分で採点する構成になってしまう。judge を変えると
スコアの水準自体が変わるため、過去のベースライン（別の judge で計測）とは
単純比較できない。

## 対処

ベースラインと比較したいときは、必ず judge に使いたいモデルを先頭にして
複数モデルを渡す：

```bash
go run ./cmd/llmo eval -models qwen2.5:7b,ornith:9b -conditions no-rag
```

これで `qwen2.5:7b` が judge、`ornith:9b`（と `qwen2.5:7b` 自身）が
評価対象として実行される。`-models` を省略すれば config.yaml の
`router,knowledge` 順がデフォルトで使われ、同じ構成になる。
