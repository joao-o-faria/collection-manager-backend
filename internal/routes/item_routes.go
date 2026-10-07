package routes

import (
	"collection-manager-backend/internal/handlers"
	"collection-manager-backend/internal/middleware"

	"github.com/gin-gonic/gin"
)

func RegisterItemRoutes(r *gin.Engine) {
	items := r.Group("/items")
	items.Use(middleware.AuthMiddleware())
	{
		items.POST("", handlers.CreateItem)
		items.POST("/suggest", handlers.SuggestItemDetails)
		items.GET("", handlers.GetItemsByCollection)
		items.PUT("/:id", handlers.UpdateItem)
		items.DELETE("/:id", handlers.DeleteItem)
	}
}
