---
title: "YUKI が ornith:9b eval 中に熱シャットダウンループに入る"
tags: [ollama, gpu, thermal, windows, eval]
severity: high
date: "2026-08-22"
---

## 症状

ornith:9b × 4条件 × 21ケース (= 63リクエスト) を連続実行すると
YUKI (RTX4070, 192.168.68.56) がシャットダウンする。
eval runner が並列リクエストを送るとモデルスワップ (ornith↔qwen) が発生し
GPU が 190W / CPU ThermalState=3 に達して ACPI 熱シャットダウン。

## 原因

1. judge/compress が `qwen2.5:7b` をハードコードしていたため、ornith eval 中に
   モデルスワップが常時発生 → 2モデル同時ロードで VRAM 枯渇 + 発熱
2. `OLLAMA_MAX_LOADED_MODELS` / `OLLAMA_NUM_PARALLEL` が未設定で並列ロード許可状態

## 解決策

```
# YUKI Windows 側 OllamaService 環境変数に設定
OLLAMA_MAX_LOADED_MODELS=1
OLLAMA_NUM_PARALLEL=1
```

コード側:
- `runner.go` の `judgeAccuracy` → `r.models[0]` に変更（qwen ハードコード除去）
- `runner.go` の `compressKnowledge` → `r.models[0]` に変更
- `GenerateOptions{NumPredict: 600}` で出力上限を設定
- eval ケース間に 2 秒クールダウンを追加

## 予防

ornith:9b 単独 eval 時は YUKI に 1モデルのみロードされることを前提に設計する。
`go test` 相当の連続リクエストは必ず `OLLAMA_MAX_LOADED_MODELS=1` 確認後に実行。
