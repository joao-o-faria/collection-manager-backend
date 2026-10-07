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

var quickAdd = storage.QuickAdd

type QuickAddRefInput struct {
	ID      int    `json:"id"`
	NewName string `json:"new_name"`
}

type QuickAddItemInput struct {
	Name         string             `json:"name"`
	Description  *string            `json:"description"`
	Tags         []string           `json:"tags"`
	Price        float64            `json:"price"`
	BinaryObject *BinaryObjectInput `json:"binary_object"`
}

type CreateQuickAddInput struct {
	Category   QuickAddRefInput  `json:"category"`
	Collection QuickAddRefInput  `json:"collection"`
	Item       QuickAddItemInput `json:"item"`
}

// toRefInput aceita exatamente um entre id (> 0) e new_name (não vazio).
func toRefInput(r QuickAddRefInput) (storage.RefInput, bool) {
	name := strings.TrimSpace(r.NewName)
	switch {
	case r.ID > 0 && name == "":
		return storage.RefInput{ID: r.ID}, true
	case r.ID == 0 && name != "":
		return storage.RefInput{NewName: name}, true
	default:
		return storage.RefInput{}, false
	}
}

func CreateQuickAdd(c *gin.Context) {
	userID, isAdmin, ok := actorFromContext(c)
	if !ok {
		return
	}

	var input CreateQuickAddInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	category, okCat := toRefInput(input.Category)
	collection, okCol := toRefInput(input.Collection)
	if !okCat || !okCol {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Informe id ou nome de categoria e coleção"})
		return
	}
	if category.ID == 0 && collection.ID != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Categoria nova exige coleção nova"})
		return
	}
	name := strings.TrimSpace(input.Item.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nome não pode ser vazio"})
		return
	}
	if input.Item.Price < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Valor inválido"})
		return
	}

	item, err := quickAdd(c.Request.Context(), userID, isAdmin, storage.QuickAddInput{
		Category:    category,
		Collection:  collection,
		Name:        name,
		Description: normalizeDescription(input.Item.Description),
		Tags:        normalizeTags(input.Item.Tags),
		Price:       input.Item.Price,
		Binary:      toPayload(input.Item.BinaryObject),
	})
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "Categoria ou coleção não encontrada"})
		case errors.Is(err, storage.ErrCategoryMismatch):
			c.JSON(http.StatusBadRequest, gin.H{"error": "A coleção não pertence à categoria informada"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao cadastrar item"})
		}
		return
	}

	c.JSON(http.StatusCreated, item)
}
