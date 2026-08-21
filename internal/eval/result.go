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
	Accuracy         float64   `json:"accuracy"`       // 0.0-1.0, -1 = not scored
	Answer           string    `json:"answer"`
	Error            string    `json:"error,omitempty"`
}

// SummaryKey groups results by model+condition for aggregate stats.
type SummaryKey struct {
	Model     string
	Condition Condition
}

type Summary struct {
	Model         string    `json:"model"`
	Condition     Condition `json:"condition"`
	Count         int       `json:"count"`
	AvgLatencyMS  float64   `json:"avg_latency_ms"`
	TotalTokens   int       `json:"total_tokens"`
	AvgRecall     float64   `json:"avg_recall"`
	AvgAccuracy   float64   `json:"avg_accuracy"` // -1 if not scored
	ErrorCount    int       `json:"error_count"`
}

type RunSummary struct {
	RunAt     time.Time `json:"run_at"`
	Results   []Result  `json:"results"`
	Summaries []Summary `json:"summaries"`
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

func ComputeSummaries(results []Result) []Summary {
	type agg struct {
		latency  int64
		tokens   int
		recall   float64
		accuracy float64
		accCount int
		errors   int
		count    int
	}
	m := map[SummaryKey]*agg{}
	for _, r := range results {
		k := SummaryKey{r.Model, r.Condition}
		if m[k] == nil {
			m[k] = &agg{}
		}
		a := m[k]
		a.count++
		a.latency += r.LatencyMS
		a.tokens += r.PromptTokens + r.CompletionTokens
		a.recall += r.KeywordRecall
		if r.Accuracy >= 0 {
			a.accuracy += r.Accuracy
			a.accCount++
		}
		if r.Error != "" {
			a.errors++
		}
	}

	var summaries []Summary
	for k, a := range m {
		avgAcc := -1.0
		if a.accCount > 0 {
			avgAcc = a.accuracy / float64(a.accCount)
		}
		summaries = append(summaries, Summary{
			Model:        k.Model,
			Condition:    k.Condition,
			Count:        a.count,
			AvgLatencyMS: float64(a.latency) / float64(a.count),
			TotalTokens:  a.tokens,
			AvgRecall:    a.recall / float64(a.count),
			AvgAccuracy:  avgAcc,
			ErrorCount:   a.errors,
		})
	}
	return summaries
}

func PrintTable(results []Result) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "MODEL\tCONDITION\tCASE_ID\tLATENCY\tTOKENS\tRECALL\tACCURACY\tERROR")
	fmt.Fprintln(w, "-----\t---------\t-------\t-------\t------\t------\t--------\t-----")
	for _, r := range results {
		errStr := ""
		if r.Error != "" {
			if len(r.Error) > 30 {
				errStr = r.Error[:30] + "..."
			} else {
				errStr = r.Error
			}
		}
		accStr := "n/a"
		if r.Accuracy >= 0 {
			accStr = fmt.Sprintf("%.2f", r.Accuracy)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%dms\t%d\t%.2f\t%s\t%s\n",
			r.Model, r.Condition, r.CaseID,
			r.LatencyMS, r.PromptTokens+r.CompletionTokens,
			r.KeywordRecall, accStr, errStr)
	}
	w.Flush()
}

func PrintSummary(summaries []Summary) {
	fmt.Println("\n=== Summary (per model × condition) ===")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "MODEL\tCONDITION\tCOUNT\tAVG_LATENCY\tTOTAL_TOKENS\tAVG_RECALL\tAVG_ACCURACY\tERRORS")
	fmt.Fprintln(w, "-----\t---------\t-----\t-----------\t------------\t----------\t------------\t------")
	for _, s := range summaries {
		accStr := "n/a"
		if s.AvgAccuracy >= 0 {
			accStr = fmt.Sprintf("%.2f", s.AvgAccuracy)
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%.0fms\t%d\t%.2f\t%s\t%d\n",
			s.Model, s.Condition, s.Count,
			s.AvgLatencyMS, s.TotalTokens,
			s.AvgRecall, accStr, s.ErrorCount)
	}
	w.Flush()
}

func SaveJSON(results []Result, path string) error {
	summaries := ComputeSummaries(results)
	summary := RunSummary{RunAt: time.Now(), Results: results, Summaries: summaries}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
