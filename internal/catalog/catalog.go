// Package catalog discovers the models OpenCode can use and the reasoning
// effort variants gentle-ai cached for them.
package catalog

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Catalog maps provider ID to its model IDs, plus optional effort variants.
type Catalog struct {
	Providers map[string][]string            // provider -> model IDs (newest first)
	Variants  map[string]map[string][]string // provider -> model -> variants
}

// ProviderIDs returns provider IDs sorted alphabetically.
func (c Catalog) ProviderIDs() []string {
	ids := make([]string, 0, len(c.Providers))
	for id := range c.Providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Efforts returns the reasoning effort variants for provider/model, if known.
func (c Catalog) Efforts(provider, model string) []string {
	if c.Variants == nil {
		return nil
	}
	return c.Variants[provider][model]
}

// ParseModels parses `opencode models` output (one "provider/model" per line).
func ParseModels(out []byte) map[string][]string {
	providers := map[string][]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		provider, model, ok := strings.Cut(line, "/")
		if !ok || provider == "" || model == "" || strings.ContainsAny(provider, " \t") {
			continue
		}
		providers[provider] = append(providers[provider], model)
	}
	for p, models := range providers {
		providers[p] = SortNewestFirst(dedupe(models))
	}
	return providers
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// LoadVariants reads gentle-ai's model-variants.json cache. A missing file
// yields an empty map and no error.
func LoadVariants(path string) (map[string]map[string][]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]map[string][]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var v map[string]map[string][]string
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return v, nil
}

// Discover runs `opencode models` (binary overridable via bin) and loads the
// variants cache. The variants error is non-fatal and returned separately.
func Discover(ctx context.Context, bin, variantsFile string) (Catalog, error, error) {
	if bin == "" {
		bin = "opencode"
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "models")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			err = fmt.Errorf("%w: %s", err, msg)
		}
		return Catalog{}, fmt.Errorf("opencode models: %w", err), nil
	}
	c := Catalog{Providers: ParseModels(out)}
	variants, verr := LoadVariants(variantsFile)
	c.Variants = variants
	return c, nil, verr
}

var versionPattern = regexp.MustCompile(`\d+(?:[._-]\d+)*`)

// SortNewestFirst orders model IDs by the first version number found in
// them, highest first (same heuristic as gentle-ai 3.7), ties alphabetical.
func SortNewestFirst(models []string) []string {
	sorted := append([]string(nil), models...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if c := compareKeys(versionKey(sorted[i]), versionKey(sorted[j])); c != 0 {
			return c > 0
		}
		return sorted[i] < sorted[j]
	})
	return sorted
}

func versionKey(id string) []int {
	var best []int
	for _, match := range versionPattern.FindAllString(strings.ToLower(id), -1) {
		parts := strings.FieldsFunc(match, func(r rune) bool { return r == '.' || r == '_' || r == '-' })
		key := make([]int, 0, len(parts))
		for _, part := range parts {
			if v, err := strconv.Atoi(part); err == nil {
				key = append(key, v)
			}
		}
		if compareKeys(key, best) > 0 {
			best = key
		}
		if len(key) > 0 && key[0] < 1000 {
			return key
		}
	}
	return best
}

func compareKeys(a, b []int) int {
	n := max(len(a), len(b))
	for i := 0; i < n; i++ {
		var l, r int
		if i < len(a) {
			l = a[i]
		}
		if i < len(b) {
			r = b[i]
		}
		if l != r {
			if l > r {
				return 1
			}
			return -1
		}
	}
	return 0
}
