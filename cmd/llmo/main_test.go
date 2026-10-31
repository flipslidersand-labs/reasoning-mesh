package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
)

func TestResolveConditions_Valid(t *testing.T) {
	got := resolveConditions("cosine,score")
	want := []eval.Condition{eval.CondCosine, eval.CondScore}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("resolveConditions(%q) = %v, want %v", "cosine,score", got, want)
	}
}

func TestResolveConditions_All(t *testing.T) {
	if got := resolveConditions("all"); !reflect.DeepEqual(got, eval.AllConditions) {
		t.Errorf("resolveConditions(\"all\") = %v, want %v", got, eval.AllConditions)
	}
	if got := resolveConditions(""); !reflect.DeepEqual(got, eval.AllConditions) {
		t.Errorf("resolveConditions(\"\") = %v, want %v", got, eval.AllConditions)
	}
}

func TestSplitComma(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"no spaces", "foo,bar", []string{"foo", "bar"}},
		{"leading/trailing spaces", "foo, bar", []string{"foo", "bar"}},
		{"spaces around all elements", " foo , bar , baz ", []string{"foo", "bar", "baz"}},
		{"empty string", "", []string{""}},
		{"single value with spaces", "  foo  ", []string{"foo"}},
		{"consecutive separators", "a,,b", []string{"a", "", "b"}},
		{"trailing separator", "a,b,", []string{"a", "b", ""}},
		{"leading separator", ",a", []string{"", "a"}},
		{"only separator", ",", []string{"", ""}},
		{"whitespace only", " \t ", []string{""}},
		{"whitespace-only element", "a, ,b", []string{"a", "", "b"}},
		{"unicode space", "a,\u3000b\u3000", []string{"a", "b"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitComma(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("splitComma(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

// splitComma は strings.Split + TrimSpace と同一挙動であること(空文字は [""] を返す)。
func TestSplitComma_MatchesStdlib(t *testing.T) {
	inputs := []string{"", ",", ",,", "a", "a,", ",a", "a,,b", " a , b ", " ,\t, ", "a\n,b", "日本, 語", "\u00a0a\u00a0,b"}
	for _, in := range inputs {
		want := strings.Split(in, ",")
		for i := range want {
			want[i] = strings.TrimSpace(want[i])
		}
		if got := splitComma(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitComma(%q) = %#v, stdlib = %#v", in, got, want)
		}
	}
}
