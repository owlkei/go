package bench

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func NewEcho() http.Handler {
	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover())

	e.GET("/static", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })
	e.GET("/users/:id", func(c echo.Context) error { return c.JSON(http.StatusOK, map[string]any{"id": c.Param("id")}) })
	e.GET("/search", func(c echo.Context) error {
		page, _ := strconv.Atoi(c.QueryParam("page"))
		return c.JSON(http.StatusOK, map[string]any{"q": c.QueryParam("q"), "page": page})
	})
	e.POST("/users", func(c echo.Context) error {
		var input UserInput
		if err := c.Bind(&input); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": "bad request"})
		}
		return c.JSON(http.StatusCreated, map[string]any{"name": input.Name, "age": input.Age})
	})
	e.GET("/chain", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"data": map[string]any{"ok": true}})
	}, func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set(mwHeader, "1")
			return next(c)
		}
	}, func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if c.Request().Header.Get(authHeader) != authValue {
				return c.JSON(http.StatusForbidden, map[string]any{"error": "forbidden"})
			}
			return next(c)
		}
	})
	e.GET("/error", func(echo.Context) error {
		return echo.NewHTTPError(http.StatusBadRequest, "bad request")
	})
	return e
}
