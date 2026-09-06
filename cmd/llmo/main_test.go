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
