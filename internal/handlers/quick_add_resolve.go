package handlers

import (
	"strings"

	"collection-manager-backend/internal/ai"
	"collection-manager-backend/internal/models"
)

type ResolvedRef struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	IsNew bool   `json:"is_new"`
}

type QuickAddAnalysisResponse struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Tags        []string    `json:"tags"`
	Category    ResolvedRef `json:"category"`
	Collection  ResolvedRef `json:"collection"`
}

// resolveAnalysis valida a escolha do modelo contra as categorias/coleções
// realmente visíveis ao usuário, garantindo um par categoria/coleção coerente.
func resolveAnalysis(a ai.ImageAnalysis, categories []models.Category, collections []models.Collection) QuickAddAnalysisResponse {
	catByID := make(map[int]models.Category, len(categories))
	for _, c := range categories {
		catByID[c.ID] = c
	}
	colByID := make(map[int]models.Collection, len(collections))
	for _, c := range collections {
		colByID[c.ID] = c
	}

	var category ResolvedRef
	if c, ok := catByID[a.Category.ID]; ok && a.Category.ID > 0 {
		category = ResolvedRef{ID: c.ID, Name: c.Name}
	} else {
		category = ResolvedRef{Name: orDefault(a.Category.NewName, "Geral"), IsNew: true}
	}

	var collection ResolvedRef
	col, colExists := colByID[a.Collection.ID]
	switch {
	case colExists && a.Collection.ID > 0 && !category.IsNew:
		if col.CategoryID != category.ID {
			if owner, ok := catByID[col.CategoryID]; ok {
				category = ResolvedRef{ID: owner.ID, Name: owner.Name}
			}
		}
		if col.CategoryID == category.ID {
			collection = ResolvedRef{ID: col.ID, Name: col.Name}
			break
		}
		fallthrough
	default:
		newName := a.Collection.NewName
		if colExists && a.Collection.ID > 0 {
			newName = ""
		}
		collection = ResolvedRef{Name: orDefault(newName, category.Name), IsNew: true}
	}

	tags := normalizeTags(a.Tags)
	if tags == nil {
		tags = []string{}
	}

	return QuickAddAnalysisResponse{
		Name:        orDefault(a.Name, "Item sem nome"),
		Description: strings.TrimSpace(a.Description),
		Tags:        tags,
		Category:    category,
		Collection:  collection,
	}
}

func orDefault(s, fallback string) string {
	if trimmed := strings.TrimSpace(s); trimmed != "" {
		return trimmed
	}
	return fallback
}
