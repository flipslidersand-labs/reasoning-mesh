package eval

import "testing"

func TestKeywordRecall_CommaSynonymEitherMatches(t *testing.T) {
	var r Result

	got := r.keywordRecall("この設計はテスト可能です", []string{"testable,テスト可能"})
	if got != 1.0 {
		t.Errorf("recall = %.2f, want 1.0 (Japanese alternative should match)", got)
	}

	got = r.keywordRecall("this design is testable", []string{"testable,テスト可能"})
	if got != 1.0 {
		t.Errorf("recall = %.2f, want 1.0 (English alternative should match)", got)
	}

	got = r.keywordRecall("this design is flexible", []string{"testable,テスト可能"})
	if got != 0.0 {
		t.Errorf("recall = %.2f, want 0.0 (neither alternative present)", got)
	}
}

func TestKeywordRecall_CommaSynonymDoesNotInflateDenominator(t *testing.T) {
	var r Result

	// A synonym group still counts as exactly one required keyword.
	got := r.keywordRecall("testable interface config", []string{"config", "testable,テスト可能", "interface,インターフェース"})
	if got != 1.0 {
		t.Errorf("recall = %.2f, want 1.0 (3 keywords, all matched via one alternative each)", got)
	}
}
