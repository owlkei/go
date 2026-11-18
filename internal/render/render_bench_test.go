package render

import (
	"bytes"
	"html/template"
	"testing"
)

func BenchmarkRender_PooledBuffer(b *testing.B) {
	e := rdEngine(b, benchPage(false), false)
	data := benchRowsData()
	b.Run("pooled", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			buf, err := e.Render("/list", data)
			if err != nil {
				b.Fatal(err)
			}
			e.Release(buf)
		}
	})
	b.Run("new buffer", func(b *testing.B) {
		b.ReportAllocs()
		t := template.Must(template.New("list").Parse(benchPage(false)["list.html"]))
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
				buf, err := e.Render("/list", data)
				if err != nil {
					b.Fatal(err)
				}
				e.Release(buf)
			}
		})
	}
}
