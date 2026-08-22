---
title: "Qdrant upsert が 400 — point ID は UUID (32hex) か uint64 のみ"
tags: [qdrant, go, uuid, upsert]
severity: high
date: "2026-08-22"
---

## 症状

Qdrant REST API の `/collections/{name}/points` に PUT すると 400 Bad Request。

## 原因

`deterministicID` が SHA256 の full hex (64文字) を返していた。
Qdrant の point ID は UUID 形式 (32文字 hex) または uint64 のみ受け付ける。

## 解決策

```go
func deterministicID(taskType eval.TaskType, content string) string {
    contentHash := sha256.Sum256([]byte(content))
    combined := string(taskType) + "|" + fmt.Sprintf("%x", contentHash)
    h := sha256.Sum256([]byte(combined))
    return fmt.Sprintf("%x", h[:16])  // 先頭16バイト = 32文字 UUID
}
```

## 予防

Qdrant に point ID を渡す関数は必ず `len(id) == 32` をアサートするか
`uuid.New().String()` (ハイフン除去) を使う。
