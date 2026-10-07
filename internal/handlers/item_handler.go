package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"collection-manager-backend/internal/storage"

	"github.com/gin-gonic/gin"
)

func normalizeDescription(d *string) *string {
	if d == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*d)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		trimmed := strings.TrimSpace(t)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

type CreateItemInput struct {
	Name         string             `json:"name" binding:"required"`
	Description  *string            `json:"description"`
	Tags         []string           `json:"tags"`
	Price        float64            `json:"price"`
	CollectionID int                `json:"collection_id" binding:"required"`
	BinaryObject *BinaryObjectInput `json:"binary_object"`
}

type UpdateItemInput struct {
	Name         string             `json:"name" binding:"required"`
	Description  *string            `json:"description"`
	Tags         []string           `json:"tags"`
	Price        float64            `json:"price"`
	CollectionID int                `json:"collection_id" binding:"required"`
	BinaryObject *BinaryObjectInput `json:"binary_object"`
}

func CreateItem(c *gin.Context) {
	userID, isAdmin, ok := actorFromContext(c)
	if !ok {
		return
	}

	var input CreateItemInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	item, err := storage.AddItem(c.Request.Context(), userID, isAdmin, input.Name, normalizeDescription(input.Description), normalizeTags(input.Tags), input.Price, input.CollectionID, toPayload(input.BinaryObject))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Coleção não encontrada"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao salvar item"})
		return
	}

	notifyItemSaved(item.ID)
	c.JSON(http.StatusCreated, item)
}

func GetItemsByCollection(c *gin.Context) {
	userID, isAdmin, ok := actorFromContext(c)
	if !ok {
		return
	}

	idParam := c.Query("collection_id")
	collectionID := 0
	if _, err := fmt.Sscanf(idParam, "%d", &collectionID); err != nil || collectionID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "collection_id inválido"})
		return
	}

	items, err := storage.GetItemsByCollection(c.Request.Context(), userID, isAdmin, collectionID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Coleção não encontrada"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar itens"})
		return
	}

	c.JSON(http.StatusOK, items)
}

func UpdateItem(c *gin.Context) {
	userID, isAdmin, ok := actorFromContext(c)
	if !ok {
		return
	}

	idParam := c.Param("id")
	id := 0
	if _, err := fmt.Sscanf(idParam, "%d", &id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}

	var input UpdateItemInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	item, err := storage.UpdateItem(c.Request.Context(), userID, isAdmin, id, input.Name, normalizeDescription(input.Description), normalizeTags(input.Tags), input.Price, input.CollectionID, toPayload(input.BinaryObject))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Item ou coleção não encontrada"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao atualizar item"})
		return
	}

	notifyItemSaved(item.ID)
	c.JSON(http.StatusOK, item)
}

func DeleteItem(c *gin.Context) {
	userID, isAdmin, ok := actorFromContext(c)
	if !ok {
		return
	}

	idParam := c.Param("id")
	id := 0
	if _, err := fmt.Sscanf(idParam, "%d", &id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}

	if err := storage.DeleteItem(c.Request.Context(), userID, isAdmin, id); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Item não encontrado"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao deletar item"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Item deletado com sucesso"})
}
