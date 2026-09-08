---
title: "git fetch しただけのローカルmainを信じて編集すると、squash-mergeされた変更を消しかける"
category: system
date: "2026-09-07"
---

## 症状

メインチェックアウト（worktreeではない `reasoning-mesh/` 直下）で
`internal/trigger/handler.go` を Read → 編集したところ、直前に別PRで
squash-mergeされていた `validate.CommitSHA` 検証と
`subtle.ConstantTimeCompare` によるタイミング攻撃対策を、気づかず削除する
差分を作ってしまった。CIの既存テスト（`TestHandler_InvalidCommitSHA`）が
落ちたことで発覚。

## 原因

`git fetch origin main` はリモート追跡ブランチ `origin/main` を更新するだけで、
ローカルの `main` ブランチ（作業ツリーの実体）は自動更新されない。複数PRを
連続でsquash-mergeした直後、メインチェックアウトの `git checkout main` 後の
ファイル内容は「マージ前の古いmain」のままになりうる。そこをそのまま Read して
編集の土台にすると、後から入った変更を静かに巻き戻す差分になる。

## 対処

- 新しい作業を始める前は、必ず `git fetch origin main --quiet` した上で
  `origin/main` から worktree を作る（`git worktree add ... origin/main`）。
  ローカル `main` ブランチのチェックアウトを直接編集の土台にしない。
- 既存ファイルをベースに変更を作るときは `git show origin/main:<path>` で
  実際の最新内容を確認してから編集する。
- 疑わしい場合はテストを通してから気づくのではなく、diffで「消えている行」が
  ないか rebase 直後に確認する。
