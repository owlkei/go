// Command pages is a small server-rendered booking site: Owlky page routes,
// html/template views and Owlky's framework-free HTML components.
package main

import (
	"embed"
	"io/fs"
	"os"
	"strconv"

	"go.owlky.com/owlky"
	"go.owlky.com/owlky/middlewares/csrf"
	"go.owlky.com/owlky/middlewares/helmet"
)

//go:embed web
var web embed.FS

const defaultPort = 8080

// newApp builds the application on the templates and assets of the web directory (RenderOptions.Dir
// defaults to "web"). With reload, the same directory is read from disk and reloaded on change.
func newApp(files fs.FS, reload bool) owlky.App {
	app := owlky.New()
	app.BindGlobalMiddlewares(helmet.Helmet{}, csrf.CSRF{})
	app.EnableRender(&owlky.RenderOptions{FS: files, IsReload: reload})
	app.Create(Module())
	return app
}

func main() {
	// PAGES_DEV=1 reads web/ from disk and reloads it on change. Run it from this folder.
	reload := os.Getenv("PAGES_DEV") == "1"

	port := defaultPort
	if v := os.Getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			panic("PORT must be a number: " + v)
		}
		port = p
	}

	app := newApp(web, reload)
	if err := app.Listen(port, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
