package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/config"
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

func TestResolveModels(t *testing.T) {
	full := &config.Config{}
	full.Ollama.Models = map[string]string{"router": "r:1b", "knowledge": "k:9b"}
	noKeys := &config.Config{}
	noKeys.Ollama.Models = map[string]string{}
	onlyRouter := &config.Config{}
	onlyRouter.Ollama.Models = map[string]string{"router": "r:1b"}
	blankValues := &config.Config{}
	blankValues.Ollama.Models = map[string]string{"router": " ", "knowledge": ""}

	cases := []struct {
		name    string
		cfg     *config.Config
		flag    string
		want    []string
		wantErr bool
	}{
		{"flag overrides config", full, "x,y", []string{"x", "y"}, false},
		{"flag single", full, "x", []string{"x"}, false},
		{"flag trims spaces", full, " x , y ", []string{"x", "y"}, false},
		{"flag drops empty elements", full, "a,,b", []string{"a", "b"}, false},
		{"flag drops whitespace-only and trailing", full, ",a, ,b,", []string{"a", "b"}, false},
		{"flag only separators is an error", full, ",", nil, true},
		{"empty flag uses router and knowledge", full, "", []string{"r:1b", "k:9b"}, false},
		{"empty flag drops one missing key", onlyRouter, "", []string{"r:1b"}, false},
		{"empty flag with missing keys is an error", noKeys, "", nil, true},
		{"empty flag with blank values is an error", blankValues, "", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveModels(tc.cfg, tc.flag)
			if (err != nil) != tc.wantErr {
				t.Fatalf("resolveModels(%q) err = %v, wantErr %v", tc.flag, err, tc.wantErr)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("resolveModels(%q) = %#v, want %#v", tc.flag, got, tc.want)
			}
		})
	}
}

func TestResolveModels_NilModelsMap(t *testing.T) {
	cfg := &config.Config{} // nil map: reads return "" without panic
	got, err := resolveModels(cfg, "")
	if err == nil {
		t.Fatalf("resolveModels = %#v, want error", got)
	}
	if !strings.Contains(err.Error(), "ollama.models") {
		t.Errorf("error %q should mention ollama.models", err)
	}
}
