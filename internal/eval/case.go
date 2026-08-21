package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type TaskType string

const (
	TaskDebugging      TaskType = "debugging"
	TaskImplementation TaskType = "implementation"
	TaskArchitecture   TaskType = "architecture"
)

type Case struct {
	ID       string   `yaml:"id"`
	TaskType TaskType `yaml:"task_type"`
	Prompt   string   `yaml:"prompt"`
	Context  struct {
		Language  string `yaml:"language"`
		Framework string `yaml:"framework"`
		Snippet   string `yaml:"snippet"`
	} `yaml:"context"`
	Expected struct {
		RootCause        string   `yaml:"root_cause"`
		RequiredKeywords []string `yaml:"required_keywords"`
		ForbiddenKeywords []string `yaml:"forbidden_keywords"`
	} `yaml:"expected"`
}

func LoadCases(dir string) ([]Case, error) {
	var cases []Case
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		if filepath.Base(path) == "schema.yaml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		var c Case
		if err := yaml.Unmarshal(data, &c); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		cases = append(cases, c)
		return nil
	})
	return cases, err
}
