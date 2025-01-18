package middleware

import (
	"PiliPili_Backend/logger"
	"bytes"
	"github.com/gin-gonic/gin"
	"io"
)

// CorsMiddleware handles Cross-Origin Resource Sharing (CORS) headers.
func CorsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		logger.Info("Incoming request details:")
		logger.Info("Request Method: %s", c.Request.Method)
		logger.Info("Request Path: %s", c.Request.URL.Path)
		logger.Info("Request Headers: %v", c.Request.Header)

		if c.Request.Method == "POST" || c.Request.Method == "PUT" {
			body, err := io.ReadAll(c.Request.Body)
			if err != nil {
				logger.Error("Error reading request body: %v", err)
			} else {
				c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
				logger.Info("Request Body: %s", body)
			}
		}

		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		logger.Info("Setting CORS headers for request: %s %s", c.Request.Method, c.Request.URL.Path)
		logger.Info("Response Headers: %v", c.Writer.Header())

		if c.Request.Method == "OPTIONS" {
			logger.Error("OPTIONS request received, aborting with status 204")
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
