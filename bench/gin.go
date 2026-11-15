package bench

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func NewGin() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

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
	router.GET("/error", func(c *gin.Context) { c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"}) })
	return router
}
