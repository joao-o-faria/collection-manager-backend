package routes

import (
	"collection-manager-backend/internal/handlers"
	"collection-manager-backend/internal/middleware"

	"github.com/gin-gonic/gin"
)

func RegisterQuickAddRoutes(r *gin.Engine) {
	quickAdd := r.Group("/quick-add")
	quickAdd.Use(middleware.AuthMiddleware())
	{
		quickAdd.POST("/analyze", handlers.AnalyzeQuickAdd)
	}
}
