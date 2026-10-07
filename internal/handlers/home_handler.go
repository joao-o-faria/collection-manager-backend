package handlers

import (
	"net/http"

	"collection-manager-backend/internal/models"
	"collection-manager-backend/internal/search"
	"collection-manager-backend/internal/storage"

	"github.com/gin-gonic/gin"
)

var (
	loadHomeTotals  = storage.GetHomeTotals
	loadRecentItems = storage.GetRecentItems
	loadItemTags    = storage.GetItemTags
)

type HomeSummaryResponse struct {
	Totals      storage.HomeTotals `json:"totals"`
	RecentItems []models.Item      `json:"recent_items"`
	TopTags     []string           `json:"top_tags"`
}

func GetHomeSummary(c *gin.Context) {
	// Painel pessoal: só os dados do próprio usuário (admin incluído).
	userID, _, ok := actorFromContext(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()

	totals, err := loadHomeTotals(ctx, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao carregar resumo"})
		return
	}
	recent, err := loadRecentItems(ctx, userID, 6)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao carregar resumo"})
		return
	}
	tags, err := loadItemTags(ctx, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao carregar resumo"})
		return
	}
	if recent == nil {
		recent = []models.Item{}
	}

	c.JSON(http.StatusOK, HomeSummaryResponse{
		Totals:      totals,
		RecentItems: recent,
		TopTags:     search.TopTags(tags, 5),
	})
}
