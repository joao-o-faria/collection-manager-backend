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
	semantic, scoreOf, semanticOK := semanticSearch(ctx, userID, q)

	// A busca textual roda sempre: embeddings são fracos com palavras soltas
	// ("luvas" mal pontua "Luvas de Boxe"), e quem digita o nome espera achá-lo.
	textItems, err := searchItemsText(ctx, userID, q, search.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar itens"})
		return
	}

	if !semanticOK {
		results := make([]SearchResult, 0, len(textItems))
		for _, item := range textItems {
			results = append(results, SearchResult{Item: item})
		}
		c.JSON(http.StatusOK, SearchResponse{Mode: "text", Results: results})
		return
	}

	// Híbrida: correspondências literais primeiro, depois as por significado, sem repetir.
	results := make([]SearchResult, 0, search.Limit)
	seen := make(map[int]bool)
	for _, item := range textItems {
		results = append(results, SearchResult{Item: item, Score: scoreOf(item.ID)})
		seen[item.ID] = true
	}
	for _, r := range semantic {
		if !seen[r.Item.ID] {
			results = append(results, r)
			seen[r.Item.ID] = true
		}
	}
	if len(results) > search.Limit {
		results = results[:search.Limit]
	}
	c.JSON(http.StatusOK, SearchResponse{Mode: "semantic", Results: results})
}

// semanticSearch devolve ok=false quando a busca semântica não é possível
// (IA indisponível ou nenhum item indexado ainda). scoreOf dá a similaridade de
// qualquer item indexado do usuário com a consulta (nil se ele não tiver vetor).
func semanticSearch(ctx context.Context, userID uint, q string) ([]SearchResult, func(int) *float32, bool) {
	noScore := func(int) *float32 { return nil }
	if queryEmbed == nil {
		return nil, noScore, false
	}
	candidates, err := loadItemEmbeddings(ctx, userID)
	if err != nil || len(candidates) == 0 {
		return nil, noScore, false
	}
	vec, err := queryEmbed.EmbedQuery(ctx, q)
	if err != nil {
		log.Printf("erro na IA (busca): %v", err)
		return nil, noScore, false
	}

	vectors := make(map[int][]float32, len(candidates))
	for _, cand := range candidates {
		vectors[cand.ItemID] = cand.Vector
	}
	scoreOf := func(id int) *float32 {
		v, ok := vectors[id]
		if !ok {
			return nil
		}
		s := search.Cosine(vec, v)
		return &s
	}

	ranked := search.Rank(vec, candidates, search.MinScore, search.RelativeToTop, search.Limit)
	ids := make([]int, len(ranked))
	scores := make(map[int]float32, len(ranked))
	for i, r := range ranked {
		ids[i] = r.ItemID
		scores[r.ItemID] = r.Score
	}

	items, err := loadItemsByIDs(ctx, userID, ids)
	if err != nil {
		return nil, noScore, false
	}
	results := make([]SearchResult, 0, len(items))
	for _, item := range items {
		score := scores[item.ID]
		results = append(results, SearchResult{Item: item, Score: &score})
	}
	return results, scoreOf, true
}
