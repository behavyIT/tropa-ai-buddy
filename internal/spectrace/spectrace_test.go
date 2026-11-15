// Package spectrace verifies Spec-Driven Development traceability: every
// scenario ID declared in openspec/specs/**/spec.md must be exercised by at
// least one automated test (Go test name "TestABS02_…" or Vitest title "[UI-07] …").
package spectrace

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var scenarioRe = regexp.MustCompile(`#### Scenario: \[([A-Z]+)-(\d+)\]`)

func repoRoot(t *testing.T) string {
	dir, _ := os.Getwd()
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("repo root not found")
	return ""
}

func TestSpecScenariosHaveTests(t *testing.T) {
	root := repoRoot(t)
	ids := map[string]string{} // "ABS-02" -> spec file
	_ = filepath.WalkDir(filepath.Join(root, "openspec", "specs"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "spec.md" {
			return err
		}
		b, _ := os.ReadFile(p)
		for _, m := range scenarioRe.FindAllStringSubmatch(string(b), -1) {
			ids[m[1]+"-"+m[2]] = strings.TrimPrefix(p, root+string(os.PathSeparator))
		}
		return nil
	})
	if len(ids) < 50 {
		t.Fatalf("too few scenarios parsed: %d", len(ids))
	}
	var corpus strings.Builder
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == "coverage") {
			return filepath.SkipDir
		}
		n := d.Name()
		if strings.HasSuffix(n, "_test.go") || strings.HasSuffix(n, ".test.ts") || strings.HasSuffix(n, ".test.tsx") {
			b, _ := os.ReadFile(p)
			corpus.Write(b)
		}
		return nil
	})
	tests := corpus.String()
	var missing, matrix []string
	keys := make([]string, 0, len(ids))
	for id := range ids {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		goName := "func Test" + strings.ReplaceAll(id, "-", "") + "_"
		tsName := "[" + id + "]"
		switch {
		case strings.Contains(tests, goName):
			matrix = append(matrix, fmt.Sprintf("| %s | %s | Go: `Test%s_*` |", id, ids[id], strings.ReplaceAll(id, "-", "")))
		case strings.Contains(tests, tsName):
			matrix = append(matrix, fmt.Sprintf("| %s | %s | Vitest: `%s` |", id, ids[id], tsName))
		default:
			missing = append(missing, id+" ("+ids[id]+")")
		}
	}
	if len(missing) > 0 {
		t.Fatalf("scenarios without tests: %s", strings.Join(missing, ", "))
	}
	if os.Getenv("SPEC_MATRIX") == "1" {
		out := "# Трассируемость: сценарий спецификации → автотест\n\nСгенерировано `SPEC_MATRIX=1 go test ./internal/spectrace`. Сценариев: " +
			fmt.Sprint(len(keys)) + ".\n\n| ID | Спецификация | Тест |\n|---|---|---|\n" + strings.Join(matrix, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(root, "openspec", "TRACEABILITY.md"), []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("%d scenarios traced to tests", len(keys))
}
