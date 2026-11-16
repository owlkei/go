package ds

import (
	"testing"
)

func BenchmarkMatchGlob(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		matchGlob("index.html", "in*.html")
	}
}

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

func BenchmarkTrieFind_Static(b *testing.B) {
	tr := buildBenchTrie()
	b.ReportAllocs()
	for b.Loop() {
		tr.Find("/feeds/all", nil)
	}
}

func BenchmarkTrieFind_WithParam(b *testing.B) {
	tr := buildBenchTrie()
	b.ReportAllocs()
	for b.Loop() {
		tr.Find("/users/633b0aa5d7fc3578b655b9bd", nil)
	}
}

func BenchmarkTrieFind_DeepParam(b *testing.B) {
	tr := buildBenchTrie()
	b.ReportAllocs()
	for b.Loop() {
		tr.Find("/users/633b0aa5d7fc3578b655b9bd/friends/633b0af45f4fe7d45b00fba5", nil)
	}
}

func BenchmarkTrieFind_ParamReusedBuffer(b *testing.B) {
	tr := buildBenchTrie()
	buf := make([]string, 0, 4)
	b.ReportAllocs()
	for b.Loop() {
		tr.Find("/users/633b0aa5d7fc3578b655b9bd/friends/633b0af45f4fe7d45b00fba5", buf[:0])
	}
}

func BenchmarkTrieFind_NoMatch(b *testing.B) {
	tr := buildBenchTrie()
	b.ReportAllocs()
	for b.Loop() {
		tr.Find("/notexist/route", nil)
	}
}

func BenchmarkTrieFind_Backtrack(b *testing.B) {
	tr := buildBenchTrie()
	b.ReportAllocs()
	for b.Loop() {
		tr.Find("/a/1/y", nil)
	}
}

func BenchmarkTrieInsert(b *testing.B) {
	value := "v"
	b.ReportAllocs()
	for b.Loop() {
		tr := NewTrie[string]()
		_ = tr.Insert("/users/$/friends/$", &value)
		_ = tr.Insert("/feeds/all", &value)
		_ = tr.Insert("/files/*.html", &value)
	}
}
