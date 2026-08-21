package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

type Condition string

const (
	CondNoRAG      Condition = "no-rag"
	CondCosine     Condition = "cosine"
	CondScore      Condition = "score"
	CondCompressed Condition = "compressed"
)

var AllConditions = []Condition{CondNoRAG, CondCosine, CondScore, CondCompressed}

type Result struct {
	CaseID           string    `json:"case_id"`
	TaskType         TaskType  `json:"task_type"`
	Model            string    `json:"model"`
	Condition        Condition `json:"condition"`
	LatencyMS        int64     `json:"latency_ms"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	KeywordRecall    float64   `json:"keyword_recall"` // 0.0-1.0
	Answer           string    `json:"answer"`
	Error            string    `json:"error,omitempty"`
}

type RunSummary struct {
	RunAt   time.Time `json:"run_at"`
	Results []Result  `json:"results"`
}

func (r Result) keywordRecall(answer string, keywords []string) float64 {
	if len(keywords) == 0 {
		return 1.0
	}
	lower := strings.ToLower(answer)
	hit := 0
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			hit++
		}
	}
	return float64(hit) / float64(len(keywords))
}

func PrintTable(results []Result) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "MODEL\tCONDITION\tCASE_ID\tLATENCY\tTOKENS\tRECALL\tERROR")
	fmt.Fprintln(w, "-----\t---------\t-------\t-------\t------\t------\t-----")
	for _, r := range results {
		errStr := ""
		if r.Error != "" {
			errStr = r.Error[:min(len(r.Error), 30)]
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%dms\t%d\t%.2f\t%s\n",
			r.Model, r.Condition, r.CaseID,
			r.LatencyMS, r.PromptTokens+r.CompletionTokens,
			r.KeywordRecall, errStr)
	}
	w.Flush()
}

func SaveJSON(results []Result, path string) error {
	summary := RunSummary{RunAt: time.Now(), Results: results}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
