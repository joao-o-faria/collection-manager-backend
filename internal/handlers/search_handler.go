package handlers

import (
	"context"
	"log"
	"net/http"
	"strings"

	"collection-manager-backend/internal/models"
	"collection-manager-backend/internal/search"
	"collection-manager-backend/internal/storage"

	"github.com/gin-gonic/gin"
)

type queryEmbedder interface {
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
}

var (
	queryEmbed         queryEmbedder
	loadItemEmbeddings = storage.ItemEmbeddings
	loadItemsByIDs     = storage.GetItemsByIDs
	searchItemsText    = storage.SearchItemsText
)

// InitSearch define o cliente de IA que gera o vetor das consultas.
func InitSearch(e queryEmbedder) {
	queryEmbed = e
}

type SearchResult struct {
	Item  models.Item `json:"item"`
	Score *float32    `json:"score"`
}

type SearchResponse struct {
	Mode    string         `json:"mode"`
	Results []SearchResult `json:"results"`
}

func SearchItems(c *gin.Context) {
	// Como no cadastro rápido, a busca usa só os itens do próprio usuário (admin incluído).
	userID, _, ok := actorFromContext(c)
	if !ok {
		return
	}

	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Informe o que deseja buscar"})
		return
	}

	ctx := c.Request.Context()
	if results, ok := semanticSearch(ctx, userID, q); ok {
		c.JSON(http.StatusOK, SearchResponse{Mode: "semantic", Results: results})
		return
	}

	items, err := searchItemsText(ctx, userID, q, search.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar itens"})
		return
	}
	results := make([]SearchResult, 0, len(items))
	for _, item := range items {
		results = append(results, SearchResult{Item: item})
	}
	c.JSON(http.StatusOK, SearchResponse{Mode: "text", Results: results})
}

// semanticSearch devolve ok=false quando a busca semântica não é possível
// (IA indisponível ou nenhum item indexado ainda), para cair na busca textual.
func semanticSearch(ctx context.Context, userID uint, q string) ([]SearchResult, bool) {
	if queryEmbed == nil {
		return nil, false
	}
	candidates, err := loadItemEmbeddings(ctx, userID)
	if err != nil || len(candidates) == 0 {
		return nil, false
	}
	vec, err := queryEmbed.EmbedQuery(ctx, q)
	if err != nil {
		log.Printf("erro na IA (busca): %v", err)
		return nil, false
	}

	ranked := search.Rank(vec, candidates, search.MinScore, search.Limit)
	ids := make([]int, len(ranked))
	scores := make(map[int]float32, len(ranked))
	for i, r := range ranked {
		ids[i] = r.ItemID
		scores[r.ItemID] = r.Score
	}

	items, err := loadItemsByIDs(ctx, userID, ids)
	if err != nil {
		return nil, false
	}
	results := make([]SearchResult, 0, len(items))
	for _, item := range items {
		score := scores[item.ID]
		results = append(results, SearchResult{Item: item, Score: &score})
	}
	return results, true
}
