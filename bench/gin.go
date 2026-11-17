package bench

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func NewGin() http.Handler {
	return newGin(gin.Recovery())
}

func NewGinWithLog() http.Handler {
	return newGin(gin.LoggerWithConfig(gin.LoggerConfig{Output: os.Stdout, Formatter: ginJSONLine}), gin.Recovery())
}

func ginJSONLine(p gin.LogFormatterParams) string {
	return fmt.Sprintf(`{"time":%q,"status":%d,"latency":%q,"ip":%q,"method":%q,"path":%q}`+"\n",
		p.TimeStamp.Format(time.RFC3339Nano), p.StatusCode, p.Latency, p.ClientIP, p.Method, p.Path)
}

func newGin(middlewares ...gin.HandlerFunc) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(middlewares...)

	router.GET("/static", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	router.GET("/users/:id", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"id": c.Param("id")}) })
	router.GET("/search", func(c *gin.Context) {
		page, _ := strconv.Atoi(c.Query("page"))
		c.JSON(http.StatusOK, gin.H{"q": c.Query("q"), "page": page})
	})
	router.POST("/users", func(c *gin.Context) {
		var input UserInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"name": input.Name, "age": input.Age})
	})
	chain := router.Group("/", func(c *gin.Context) {
		c.Header(mwHeader, "1")
		c.Next()
	}, func(c *gin.Context) {
		if c.GetHeader(authHeader) != authValue {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		c.Next()
	})
	chain.GET("/chain", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"data": gin.H{"ok": true}}) })
	router.GET("/error", func(c *gin.Context) { c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "bad request"}) })
	return router
}
