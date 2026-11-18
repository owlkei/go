package ds

import (
	"slices"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func buildBenchTrie() *Trie[string] {
	routes := []string{
		"/users/$",
		"/users/$/friends/$",
		"/feeds/all",
		"/products/$",
		"/products/$/reviews/$",
		"/*/feeds/{feed*Id}/*/files/*.html/*",
		"/a/$/x",
		"/a/*",
	}
	tr := NewTrie[string]()
	for _, r := range routes {
		value := r
		if err := tr.Insert(r, &value); err != nil {
			panic(err)
		}
	}
	return tr
}

func newTrie(t *testing.T, patterns ...string) *Trie[string] {
	t.Helper()
	tr := NewTrie[string]()
	for _, pattern := range patterns {
		value := pattern
		if err := tr.Insert(pattern, &value); err != nil {
			t.Fatal(err)
		}
	}
	return tr
}

func find(tr *Trie[string], path string) (string, []string) {
	value, params := tr.Find(path, nil)
	if value == nil {
		return "", params
	}
	return *value, params
}

func checkFind(t *testing.T, tr *Trie[string], path, wantValue string, wantParams []string, rule string) {
	t.Helper()
	value, params := find(tr, path)
	if value != wantValue {
		t.Error(test.DiffMessage(value, wantValue, rule+": value of Find("+path+")"))
	}
	if len(params) != len(wantParams) || (len(wantParams) > 0 && !slices.Equal(params, wantParams)) {
		t.Error(test.DiffMessage(params, wantParams, rule+": params of Find("+path+")"))
	}
}

func repeatSegments(segment string, n int) string {
	return strings.Repeat("/"+segment, n)
}

func permutations(items []string) [][]string {
	if len(items) <= 1 {
		return [][]string{slices.Clone(items)}
	}
	var out [][]string
	for i := range items {
		rest := slices.Concat(items[:i], items[i+1:])
		for _, p := range permutations(rest) {
			out = append(out, append([]string{items[i]}, p...))
		}
	}
	return out
}
