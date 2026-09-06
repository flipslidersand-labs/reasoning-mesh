# CLAUDE.md

## テストカバレッジ閾値（現在: 40%）

CI の `coverage-threshold` を下げる変更は必ず PR を通す。理由（一時的な緊急対応か、
恒久的な引き下げか）を PR 本文に明記する。self-merge であっても閾値変更は
[dev-infrastructure#773](https://github.com/flipslidersand-labs/dev-infrastructure/issues/773)
の段階的引き上げ計画（3ヶ月毎に +10%、6ヶ月後に +15%・目標60-70%）を踏まえて判断する。
