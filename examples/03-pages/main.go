// Command pages is a small server-rendered booking site: Owlky page routes,
// html/template views and Owlky's framework-free HTML components.
package main

import (
	"embed"
	"io/fs"
	"log"
	"os"

	"go.owlky.com/owlky"
	"go.owlky.com/owlky/middlewares/csrf"
	"go.owlky.com/owlky/middlewares/helmet"
)

//go:embed web
var web embed.FS

// newApp builds the application on the templates and assets of the web directory (RenderOptions.Dir
// defaults to "web"). With reload, the same directory is read from disk and reloaded on change.
func newApp(files fs.FS, reload bool) owlky.App {
	app := owlky.New()
	app.BindGlobalMiddlewares(helmet.Helmet{}, csrf.CSRF{})
	app.EnableRender(&owlky.RenderOptions{FS: files, IsReload: reload})
	app.Create(NewBookingsModule())
	return app
}

func main() {
	// PAGES_DEV=1 reads web/ from disk and reloads it on change. Run it from this folder.
	reload := os.Getenv("PAGES_DEV") == "1"
	if err := newApp(web, reload).Listen(3000, nil); err != nil {
		log.Fatal(err)
	}
}
