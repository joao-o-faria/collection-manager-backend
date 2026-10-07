package routes

import (
	"collection-manager-backend/internal/handlers"
	"collection-manager-backend/internal/middleware"

	"github.com/gin-gonic/gin"
)

func RegisterHomeRoutes(r *gin.Engine) {
	r.GET("/home/summary", middleware.AuthMiddleware(), handlers.GetHomeSummary)
}
