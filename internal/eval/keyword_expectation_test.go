package eval

import "testing"

func TestKeywordExpectation_PreservesSynonymGroups(t *testing.T) {
	got := keywordExpectation([]string{"priority,優先順位", "channel", " , "})
	want := "must include: (priority または 優先順位 のいずれか), channel"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
