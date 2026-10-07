package ai

import (
	"context"
	"fmt"
	"strings"
)

type CategoryOption struct {
	ID   int
	Name string
}

type CollectionOption struct {
	ID         int
	Name       string
	CategoryID int
}

// RefSuggestion aponta para uma entidade existente (ID > 0) ou sugere criar uma nova (ID == 0, NewName).
type RefSuggestion struct {
	ID      int    `json:"id"`
	NewName string `json:"new_name"`
}

type ImageAnalysis struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Tags        []string      `json:"tags"`
	Category    RefSuggestion `json:"category"`
	Collection  RefSuggestion `json:"collection"`
}

const analyzeSystemPrompt = `Você é um assistente de um sistema de gerenciamento de coleções.
Analise a foto de um item e responda em português do Brasil com:
- "name": um nome curto para o item.
- "description": uma descrição curta e objetiva (2 a 3 frases) do que é visível (cor, material, estado de conservação). Não invente preço.
- "tags": de 3 a 6 tags curtas, em letras minúsculas.
- "category": a categoria do item. Se uma das categorias existentes servir, use {"id": <id dela>, "new_name": ""}. Se nenhuma servir, use {"id": 0, "new_name": "<nome da nova categoria>"}.
- "collection": a coleção do item. Se uma das coleções existentes servir E pertencer à categoria escolhida, use {"id": <id dela>, "new_name": ""}. Caso contrário, use {"id": 0, "new_name": "<nome da nova coleção>"}.
Prefira sempre reaproveitar categorias e coleções existentes quando fizer sentido, mesmo que os nomes não sejam idênticos.
Responda somente com o JSON pedido.`

var refSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"id":       map[string]any{"type": "integer"},
		"new_name": map[string]any{"type": "string"},
	},
	"required": []string{"id", "new_name"},
}

var analysisSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"name":        map[string]any{"type": "string"},
		"description": map[string]any{"type": "string"},
		"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"category":    refSchema,
		"collection":  refSchema,
	},
	"required": []string{"name", "description", "tags", "category", "collection"},
}

func (c *Client) AnalyzeItemImage(ctx context.Context, imageBase64 string, categories []CategoryOption, collections []CollectionOption) (ImageAnalysis, error) {
	var b strings.Builder
	b.WriteString("Categorias existentes:\n")
	if len(categories) == 0 {
		b.WriteString("(nenhuma)\n")
	}
	for _, cat := range categories {
		fmt.Fprintf(&b, "- %d: %s\n", cat.ID, cat.Name)
	}
	b.WriteString("\nColeções existentes:\n")
	if len(collections) == 0 {
		b.WriteString("(nenhuma)\n")
	}
	for _, col := range collections {
		fmt.Fprintf(&b, "- %d: %s (categoria %d)\n", col.ID, col.Name, col.CategoryID)
	}

	var analysis ImageAnalysis
	err := c.chatJSON(ctx, analyzeSystemPrompt, b.String(), imageBase64, analysisSchema, &analysis)
	return analysis, err
}
