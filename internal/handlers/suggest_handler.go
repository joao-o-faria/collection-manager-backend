package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"collection-manager-backend/internal/ai"
	"collection-manager-backend/internal/storage"

	"github.com/gin-gonic/gin"
)

type itemSuggester interface {
	SuggestItemDetails(ctx context.Context, item ai.ItemContext) (ai.ItemSuggestion, error)
}

var (
	suggester        itemSuggester
	lookupCollection = storage.GetCollection
)

// InitSuggester define o cliente de IA usado por SuggestItemDetails.
func InitSuggester(s itemSuggester) {
	suggester = s
}

type SuggestItemInput struct {
	Name         string `json:"name"`
	CollectionID int    `json:"collection_id" binding:"required"`
	ImageBase64  string `json:"image_base64"`
}

func SuggestItemDetails(c *gin.Context) {
	userID, isAdmin, ok := actorFromContext(c)
	if !ok {
		return
	}

	var input SuggestItemInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nome não pode ser vazio"})
		return
	}

	collection, err := lookupCollection(c.Request.Context(), userID, isAdmin, input.CollectionID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Coleção não encontrada"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar coleção"})
		return
	}

	if suggester == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Serviço de IA indisponível"})
		return
	}

	suggestion, err := suggester.SuggestItemDetails(c.Request.Context(), ai.ItemContext{
		Name:        name,
		Collection:  collection.Name,
		Category:    collection.Category.Name,
		ImageBase64: input.ImageBase64,
	})
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Serviço de IA indisponível"})
		return
	}

	suggestion.Description = strings.TrimSpace(suggestion.Description)
	suggestion.Tags = normalizeTags(suggestion.Tags)
	if suggestion.Tags == nil {
		suggestion.Tags = []string{}
	}

	c.JSON(http.StatusOK, suggestion)
}
