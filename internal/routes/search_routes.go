package routes

import (
	"collection-manager-backend/internal/handlers"
	"collection-manager-backend/internal/middleware"

	"github.com/gin-gonic/gin"
)

func RegisterSearchRoutes(r *gin.Engine) {
	r.GET("/search", middleware.AuthMiddleware(), handlers.SearchItems)
}
