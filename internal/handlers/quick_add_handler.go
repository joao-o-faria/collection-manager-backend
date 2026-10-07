package handlers

import (
	"context"
	"net/http"
	"strings"

	"collection-manager-backend/internal/ai"
	"collection-manager-backend/internal/storage"

	"github.com/gin-gonic/gin"
)

type imageAnalyzer interface {
	AnalyzeItemImage(ctx context.Context, imageBase64 string, categories []ai.CategoryOption, collections []ai.CollectionOption) (ai.ImageAnalysis, error)
}

var (
	analyzer        imageAnalyzer
	listCategories  = storage.GetCategories
	listCollections = storage.GetCollections
)

// InitAnalyzer define o cliente de IA usado pelo cadastro rápido.
func InitAnalyzer(a imageAnalyzer) {
	analyzer = a
}

type AnalyzeQuickAddInput struct {
	ImageBase64 string `json:"image_base64"`
}

func AnalyzeQuickAdd(c *gin.Context) {
	userID, isAdmin, ok := actorFromContext(c)
	if !ok {
		return
	}

	var input AnalyzeQuickAddInput
	if err := c.ShouldBindJSON(&input); err != nil || strings.TrimSpace(input.ImageBase64) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Imagem obrigatória"})
		return
	}

	ctx := c.Request.Context()
	categories, err := listCategories(ctx, userID, isAdmin, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar categorias"})
		return
	}
	collections, err := listCollections(ctx, userID, isAdmin, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar coleções"})
		return
	}

	if analyzer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Serviço de IA indisponível"})
		return
	}

	catOptions := make([]ai.CategoryOption, 0, len(categories))
	for _, cat := range categories {
		catOptions = append(catOptions, ai.CategoryOption{ID: cat.ID, Name: cat.Name})
	}
	colOptions := make([]ai.CollectionOption, 0, len(collections))
	for _, col := range collections {
		colOptions = append(colOptions, ai.CollectionOption{ID: col.ID, Name: col.Name, CategoryID: col.CategoryID})
	}

	analysis, err := analyzer.AnalyzeItemImage(ctx, input.ImageBase64, catOptions, colOptions)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Serviço de IA indisponível"})
		return
	}

	c.JSON(http.StatusOK, resolveAnalysis(analysis, categories, collections))
}

func CreateQuickAdd(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "não implementado"})
}
