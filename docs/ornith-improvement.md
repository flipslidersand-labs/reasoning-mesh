# ornith:9b 改善方針

基準: results/20260828-014502.json（no-rag 21件）

## 現状スコア

| condition | accuracy | keyword_recall |
|-----------|----------|---------------|
| no-rag    | 0.54     | 0.46          |
| cosine    | 0.63     | 0.53          |
| score     | 0.58     | 0.53          |

目標: no-rag accuracy ≥ 0.70

## 失敗パターン分類

accuracy < 0.6 のケース: 7件（arch-go-003 / debug-go-001 / debug-rust-001 / impl-go-002 / impl-go-003 / impl-go-006 / impl-go-007）

### Type A: キーワード不一致（3件）

正しい方向性の回答だが、eval が期待するキーワードを使わない。

| ケース | 期待 | 実際の回答 |
|--------|------|-----------|
| arch-go-003 | priority / testable / interface | 正しい設計だが用語が異なる |
| impl-go-006 | goroutine / channel / WaitGroup | 正しい実装だが用語説明なし |
| debug-go-001 | nil pointer / dereference | 正しいが説明が英語混じり |

**改善方針**:
- eval ケースの `required_keywords` を実際の回答に出やすい同義語に拡張
- または ornith fine-tune データに「キーワードを必ず使う」system prompt を追加

### Type B: ハリュシネーション（2件）

技術的に誤った回答を自信を持って出力する。

| ケース | 期待 | 誤り |
|--------|------|------|
| debug-rust-001 | lifetime `'a` を x/y 両方に束縛 | `'static` の話を混入、誤った説明 |
| arch-go-003 | interface による DI | 関係ない話を展開 |

**改善方針**:
- Rust lifetime / borrow checker の正確な説明を fine-tune データに追加
- 特に「ライフタイム不一致エラー」「borrow checker の判断」の具体例を増やす

### Type C: コードのみ出力（2件）

説明なしにコードブロックを返す。ジャッジが概念的評価をできない。

| ケース | 期待 | 実際 |
|--------|------|------|
| impl-go-003 | worker pool の設計説明 | 動くコードだが説明ゼロ |
| impl-go-002 | goroutine リーク防止の説明 | コードのみ |

**改善方針**:
- fine-tune データに「コードの前後に必ず日本語で説明を付ける」形式のサンプルを追加
- system prompt で「コードを出す場合は前後に説明を付けること」を指示

## 改善アクション

### 短期（プロンプト改善）

1. system prompt に説明要件を追加:
   ```
   コードを含む回答は必ず日本語の説明を先に書き、その後にコードを示すこと。
   技術用語（goroutine, channel, lifetime, borrow 等）は英語そのままで使うこと。
   ```

2. eval ケースの `required_keywords` を拡張:
   - `testable` → `testable, テスト可能`
   - `interface` → `interface, インターフェース`

### 中期（fine-tune データ追加）

fine-tune 対象領域（不足している分野）:
- Rust lifetime / borrow checker（現在の fine-tune データが薄い可能性）
- Go concurrency パターン（goroutine leak, WaitGroup, channel close）
- Go config management（環境変数優先度, Viper, env フォールバック）

追加方法: `go run ./cmd/llmo/ ingest-local` で Qdrant に直接投入
または ornith-tuned の再チューニング（YUKI RTX4070 で実行）

### 長期（eval 精度向上）

- judge model を qwen2.5:7b から ornith:9b 自身に変更（自己評価バイアスの検証）
- `required_keywords` ベースの評価から LLM judge 一本化を検討

## 進捗

- [x] 失敗パターン3類型の特定（2026-08-29）
- [ ] eval keywords 拡張（Type A 対応）
- [ ] Rust lifetime fine-tune データ追加（Type B 対応）
- [ ] system prompt 改善（Type C 対応）
- [ ] 改善後 eval で no-rag accuracy ≥ 0.70 確認
