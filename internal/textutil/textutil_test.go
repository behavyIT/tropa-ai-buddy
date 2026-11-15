package textutil_test

import (
	"testing"

	"tropa/internal/textutil"
)

func TestTXT01_TextPipeline(t *testing.T) {
	if textutil.Normalize("  Ёлка   ПРИВЕТ ") != "елка привет" {
		t.Fatal("normalize")
	}
	if got := textutil.Terms("Доступы к Kubernetes для стажёров"); len(got) != 3 || got[0] != "доступ" {
		t.Fatalf("terms: %v", got)
	}
	if textutil.Stem("кот") != "кот" || textutil.Stem("api") != "api" {
		t.Fatal("short/latin stems unchanged")
	}
	if !textutil.IsStopword("и") || textutil.IsStopword("доступ") {
		t.Fatal("stopwords")
	}
	if textutil.Jaccard(nil, []string{"a"}) != 0 || textutil.Jaccard([]string{"a", "b"}, []string{"b", "c"}) != 1.0/3 {
		t.Fatal("jaccard")
	}
	if textutil.Truncate("короткий", 50) != "короткий" {
		t.Fatal("no truncate")
	}
	if got := textutil.Truncate("Первое предложение. Второе длинное предложение тут.", 30); got != "Первое предложение." {
		t.Fatalf("sentence cut: %q", got)
	}
	if got := textutil.Truncate("словословословословословослово", 10); got != "словослово…" {
		t.Fatalf("hard cut: %q", got)
	}
	if textutil.ContainsAny("abc", []string{"", "z"}) {
		t.Fatal("contains")
	}
}
