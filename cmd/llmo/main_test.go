package main

import (
	"reflect"
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
