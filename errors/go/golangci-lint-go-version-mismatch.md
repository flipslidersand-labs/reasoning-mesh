---
title: "golangci-lint プリビルドバイナリが新しいGoバージョンのモジュールをlintできない"
category: go
date: "2026-09-06"
---

## 症状

`golangci-lint-action@v6` に `version: latest` を指定してCIで実行すると、
以下のエラーで即座に落ちる。

```
Error: can't load config: the Go language version (go1.24) used to build
golangci-lint is lower than the targeted Go version (1.25.0)
```

## 原因

golangci-lintの公式リリースバイナリは、そのリリース時点でビルドに使われたGoの
バージョンに縛られる。プロジェクトの `go.mod` が `go 1.25.0` を要求していても、
リリースバイナリがgo1.24でビルドされていれば新しいモジュールをlintできない
（golangci-lint本体のリリースサイクルがGoの新バージョンに追いつくまでのタイムラグ）。

## 対処

`golangci-lint-action` に `install-mode: goinstall` を追加する。プリビルド
バイナリをダウンロードする代わりに、CIランナー上の `go install` でリポジトリ自身の
Goツールチェイン（`go.mod`のバージョン）を使ってソースからビルドする。

```yaml
- name: golangci-lint
  uses: golangci/golangci-lint-action@v6
  with:
    version: latest
    install-mode: goinstall
```

ビルドに30秒程度余分にかかるが確実に動く。

## 注意

lintを初めて有効化する場合、これまで検証されていなかった既存のlint違反
（unused type、gosimple指摘等）が一気に表面化することがある。CI導入と同じPRで
まとめて直すか、`only-new-issues: true` で既存分を一時的に免除するか判断する。
