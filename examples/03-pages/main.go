// Command pages is a small server-rendered booking site: Owlky page routes,
// html/template views and Owlky's framework-free HTML components.
package main

import (
	"embed"
	"io/fs"
	"log"
	"os"

	"go.owlky.com/owlky"
)

//go:embed web
var web embed.FS

// newApp builds the application on the templates and assets of the web directory (RenderOptions.Dir
// defaults to "web"). With reload, the same directory is read from disk and reloaded on change.
func newApp(files fs.FS, reload bool) owlky.App {
	app := owlky.New(&owlky.AppOptions{
		Render:  &owlky.RenderOptions{FS: files, IsReload: reload},
		Headers: &owlky.HeadersOptions{IsEnabled: true},
		CSRF:    &owlky.CSRFOptions{IsEnabled: true},
	})
	app.Create(NewBookingsModule())
	return app
}

func main() {
	// PAGES_DEV=1 reads web/ from disk and reloads it on change. Run it from this folder.
	reload := os.Getenv("PAGES_DEV") == "1"
	if err := newApp(web, reload).Listen(); err != nil {
		log.Fatal(err)
	}
}
