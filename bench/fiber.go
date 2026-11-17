package bench

import (
	"net/http"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

const fiberJSONLine = `{"time":"${time}","status":${status},"latency":"${latency}","ip":"${ip}","method":"${method}","path":"${path}"}` + "\n"

func NewFiber() *fiber.App {
	return newFiber(recover.New())
}

func NewFiberWithLog() *fiber.App {
	return newFiber(logger.New(logger.Config{Format: fiberJSONLine, TimeFormat: time.RFC3339Nano, Output: os.Stdout, DisableColors: true}), recover.New())
}

func newFiber(handlers ...fiber.Handler) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	for _, handler := range handlers {
		app.Use(handler)
	}

	app.Get("/static", func(c *fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/users/:id", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"id": c.Params("id")}) })
	app.Get("/search", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"q": c.Query("q"), "page": c.QueryInt("page")})
	})
	app.Post("/users", func(c *fiber.Ctx) error {
		var input UserInput
		if err := c.BodyParser(&input); err != nil {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "bad request"})
		}
		return c.Status(http.StatusCreated).JSON(fiber.Map{"name": input.Name, "age": input.Age})
	})
	app.Get("/chain", func(c *fiber.Ctx) error {
		c.Set(mwHeader, "1")
		return c.Next()
	}, func(c *fiber.Ctx) error {
		if c.Get(authHeader) != authValue {
			return c.Status(http.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
		}
		return c.Next()
	}, func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"data": fiber.Map{"ok": true}})
	})
	app.Get("/error", func(*fiber.Ctx) error {
		return fiber.NewError(http.StatusBadRequest, "bad request")
	})
	return app
}
