package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseModelsGroupsByProviderNewestFirst(t *testing.T) {
	out := "openai/gpt-5\nopenai/gpt-6.1-sol\nopenrouter/anthropic/claude-3\n\nnoise line\nopenai/gpt-5\n"
	p := ParseModels([]byte(out))
	if got := strings.Join(p["openai"], ","); got != "gpt-6.1-sol,gpt-5" {
		t.Fatalf("openai = %s", got)
	}
	if got := strings.Join(p["openrouter"], ","); got != "anthropic/claude-3" {
		t.Fatalf("openrouter = %s", got)
	}
	if len(p) != 2 {
		t.Fatalf("providers = %v", p)
	}
}

func TestLoadVariants(t *testing.T) {
	dir := t.TempDir()
	missing, err := LoadVariants(filepath.Join(dir, "nope.json"))
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing file: %v %v", missing, err)
	}
	path := filepath.Join(dir, "v.json")
	if err := os.WriteFile(path, []byte(`{"openai":{"gpt-x":["low","high"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := LoadVariants(path)
	if err != nil {
		t.Fatal(err)
	}
	c := Catalog{Variants: v}
	if got := strings.Join(c.Efforts("openai", "gpt-x"), ","); got != "low,high" {
		t.Fatalf("efforts = %s", got)
	}
	if c.Efforts("openai", "other") != nil {
		t.Fatal("unknown model should have no efforts")
	}
}
