package bench

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func NewFiber() *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Use(recover.New())

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
	app.Get("/error", func(c *fiber.Ctx) error {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "bad request"})
	})
	return app
}
