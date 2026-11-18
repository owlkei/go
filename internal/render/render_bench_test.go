package render

import (
	"bytes"
	"strings"
	"testing"
)

const benchRows = 40

type benchData struct{ Rows []string }

func benchPage(nonce bool) map[string]string {
	script := ""
	if nonce {
		script = `<script nonce="{{nonce}}">1</script><style nonce="{{nonce}}"></style>`
	}
	return map[string]string{
		"layout.html": `<html><head>` + script + `</head><body>{{template "content" .}}</body></html>`,
		"list.html":   `<ul>{{range .Rows}}<li class="row">{{.}}</li>{{end}}</ul>`,
	}
}

func benchRowsData() benchData {
	rows := make([]string, benchRows)
	for i := range rows {
		rows[i] = strings.Repeat("row ", 20)
	}
	return benchData{Rows: rows}
}

func BenchmarkRender_PooledBuffer(b *testing.B) {
	e := rdEngine(b, benchPage(false), false)
	data := benchRowsData()
	b.Run("pooled", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			buf, err := e.Render("list", data)
			if err != nil {
				b.Fatal(err)
			}
			e.Release(buf)
		}
	})
	b.Run("new buffer", func(b *testing.B) {
		b.ReportAllocs()
		t := e.site.pageByName["list"]
		for b.Loop() {
			var buf bytes.Buffer
			if err := t.Execute(&buf, data); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkRender_Nonce(b *testing.B) {
	data := benchRowsData()
	for _, c := range []struct {
		name  string
		nonce bool
	}{{"without nonce", false}, {"with nonce", true}} {
		e := rdEngine(b, benchPage(c.nonce), false)
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				buf, err := e.Render("list", data)
				if err != nil {
					b.Fatal(err)
				}
				e.Release(buf)
			}
		})
	}
}
