package knowledge

import (
	"math"
	"math/rand"
	"reflect"
	"sort"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
)

func items(scores ...float64) []eval.KnowledgeItem {
	out := make([]eval.KnowledgeItem, len(scores))
	for i, s := range scores {
		out[i] = eval.KnowledgeItem{ID: string(rune('a' + i)), Score: s}
	}
	return out
}

func ids(in []eval.KnowledgeItem) string {
	s := ""
	for _, it := range in {
		s += it.ID
	}
	return s
}

func TestSortByScore(t *testing.T) {
	nan := math.NaN()
	cases := []struct {
		name string
		in   []eval.KnowledgeItem
		want string
	}{
		{"nil", nil, ""},
		{"single", items(1), "a"},
		{"already desc", items(3, 2, 1), "abc"},
		{"ascending", items(1, 2, 3), "cba"},
		{"ties keep input order", items(1, 2, 2, 1, 2), "bcead"},
		{"all equal", items(5, 5, 5), "abc"},
		{"negative and zero", items(-1, 0, -2), "bac"},
		{"+Inf first", items(1, math.Inf(1), 2), "bca"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sortByScore(tc.in)
			if got := ids(tc.in); got != tc.want {
				t.Errorf("order = %q, want %q", got, tc.want)
			}
		})
	}
	// NaN は比較が常に false。順序は不定義だが、手書き実装と同じ結果であることを固定する。
	in := items(1, nan, 3, 2)
	sortByScore(in)
	if got := ids(in); got != "abcd" {
		t.Errorf("NaN order = %q, want %q", got, "abcd")
	}
}

// ランダム入力で「Score 降順・同点は入力順(安定)」を満たすこと。
func TestSortByScore_RandomStable(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for n := 0; n < 200; n++ {
		in := make([]eval.KnowledgeItem, rng.Intn(40))
		for i := range in {
			in[i] = eval.KnowledgeItem{ID: string(rune(i + 0x100)), Score: float64(rng.Intn(5))}
		}
		want := append([]eval.KnowledgeItem(nil), in...)
		sort.SliceStable(want, func(i, j int) bool { return want[i].Score > want[j].Score })
		sortByScore(in)
		if !reflect.DeepEqual(in, want) && !(len(in) == 0 && len(want) == 0) {
			t.Fatalf("mismatch: got %v want %v", in, want)
		}
	}
}
