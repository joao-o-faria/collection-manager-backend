package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"collection-manager-backend/internal/models"
	"collection-manager-backend/internal/search"

	"github.com/gin-gonic/gin"
)

type fakeQueryEmbedder struct {
	vec []float32
	err error
	got string
}

func (f *fakeQueryEmbedder) EmbedQuery(_ context.Context, q string) ([]float32, error) {
	f.got = q
	return f.vec, f.err
}

type searchFakes struct {
	embedder     *fakeQueryEmbedder
	candidates   []search.Candidate
	items        map[int]models.Item
	textResults  []models.Item
	gotUser      uint
	gotTextQuery string
	gotIDs       []int
}

func setupSearch(t *testing.T, f *searchFakes, role models.Role, userID uint) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	prevE, prevC, prevI, prevT := queryEmbed, loadItemEmbeddings, loadItemsByIDs, searchItemsText
	if f.embedder != nil {
		queryEmbed = f.embedder
	} else {
		queryEmbed = nil
	}
	loadItemEmbeddings = func(_ context.Context, uid uint) ([]search.Candidate, error) {
		f.gotUser = uid
		return f.candidates, nil
	}
	loadItemsByIDs = func(_ context.Context, uid uint, ids []int) ([]models.Item, error) {
		f.gotIDs = ids
		var out []models.Item
		for _, id := range ids {
			if item, ok := f.items[id]; ok {
				out = append(out, item)
			}
		}
		return out, nil
	}
	searchItemsText = func(_ context.Context, uid uint, q string, limit int) ([]models.Item, error) {
		f.gotUser, f.gotTextQuery = uid, q
		return f.textResults, nil
	}
	t.Cleanup(func() { queryEmbed, loadItemEmbeddings, loadItemsByIDs, searchItemsText = prevE, prevC, prevI, prevT })

	r := gin.New()
	r.GET("/search", func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Set("user_role", role)
		SearchItems(c)
	})
	return r
}

func getSearch(r *gin.Engine, q string) (*httptest.ResponseRecorder, SearchResponse) {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/search?q="+url.QueryEscape(q), nil))
	var resp SearchResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w, resp
}

func resultIDs(resp SearchResponse) string {
	ids := make([]int, len(resp.Results))
	for i, r := range resp.Results {
		ids[i] = r.Item.ID
	}
	return fmt.Sprint(ids)
}

func TestSearchItems_SemanticRanksByCosine(t *testing.T) {
	f := &searchFakes{
		embedder: &fakeQueryEmbedder{vec: []float32{1, 0}},
		candidates: []search.Candidate{
			{ItemID: 1, Vector: []float32{0, 1}},   // 0 → abaixo do mínimo
			{ItemID: 2, Vector: []float32{1, 0.2}}, // ~0.98
			{ItemID: 3, Vector: []float32{1, 0.5}}, // ~0.89 (perto do primeiro: passa no corte relativo)
		},
		items: map[int]models.Item{1: {ID: 1}, 2: {ID: 2}, 3: {ID: 3}},
	}
	r := setupSearch(t, f, models.UserRole, 7)

	w, resp := getSearch(r, "  coisas antigas ")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	if resp.Mode != "semantic" || resultIDs(resp) != "[2 3]" {
		t.Errorf("mode = %q ids = %s", resp.Mode, resultIDs(resp))
	}
	if resp.Results[0].Score == nil || *resp.Results[0].Score < *resp.Results[1].Score {
		t.Errorf("scores = %v, %v", resp.Results[0].Score, resp.Results[1].Score)
	}
	if f.embedder.got != "coisas antigas" || f.gotUser != 7 {
		t.Errorf("query = %q user = %d", f.embedder.got, f.gotUser)
	}
}

func TestSearchItems_SkipsItemsDeletedAfterRanking(t *testing.T) {
	f := &searchFakes{
		embedder:   &fakeQueryEmbedder{vec: []float32{1, 0}},
		candidates: []search.Candidate{{ItemID: 2, Vector: []float32{1, 0}}, {ItemID: 3, Vector: []float32{1, 0.1}}},
		items:      map[int]models.Item{3: {ID: 3}},
	}
	r := setupSearch(t, f, models.UserRole, 7)

	_, resp := getSearch(r, "x")

	if resultIDs(resp) != "[3]" {
		t.Errorf("ids = %s, want [3]", resultIDs(resp))
	}
}

func TestSearchItems_FallsBackToTextWhenEmbedFails(t *testing.T) {
	f := &searchFakes{
		embedder:    &fakeQueryEmbedder{err: errors.New("model not found")},
		candidates:  []search.Candidate{{ItemID: 1, Vector: []float32{1, 0}}},
		textResults: []models.Item{{ID: 9}},
	}
	r := setupSearch(t, f, models.UserRole, 7)

	w, resp := getSearch(r, "moeda")

	if w.Code != http.StatusOK || resp.Mode != "text" || resultIDs(resp) != "[9]" {
		t.Errorf("status = %d mode = %q ids = %s", w.Code, resp.Mode, resultIDs(resp))
	}
	if resp.Results[0].Score != nil {
		t.Errorf("score = %v, want null in text mode", *resp.Results[0].Score)
	}
	if resp.Results[0].Match != "text" {
		t.Errorf("match = %q, want text", resp.Results[0].Match)
	}
	if f.gotTextQuery != "moeda" || f.embedder.got != "moeda" {
		t.Errorf("text query = %q, embed query = %q (embed must have been tried)", f.gotTextQuery, f.embedder.got)
	}
}

func TestSearchItems_FallsBackToTextWhenNoItemIsIndexedYet(t *testing.T) {
	f := &searchFakes{
		embedder:    &fakeQueryEmbedder{vec: []float32{1, 0}},
		candidates:  nil,
		textResults: []models.Item{{ID: 4}},
	}
	r := setupSearch(t, f, models.UserRole, 7)

	_, resp := getSearch(r, "moeda")

	if resp.Mode != "text" || resultIDs(resp) != "[4]" {
		t.Errorf("mode = %q ids = %s", resp.Mode, resultIDs(resp))
	}
}

func TestSearchItems_FallsBackToTextWithoutEmbedder(t *testing.T) {
	f := &searchFakes{textResults: []models.Item{}}
	r := setupSearch(t, f, models.UserRole, 7)

	w, resp := getSearch(r, "moeda")

	if w.Code != http.StatusOK || resp.Mode != "text" || resp.Results == nil {
		t.Errorf("status = %d mode = %q results = %v", w.Code, resp.Mode, resp.Results)
	}
}

func TestSearchItems_RejectsBlankQuery(t *testing.T) {
	r := setupSearch(t, &searchFakes{}, models.UserRole, 7)

	if w, _ := getSearch(r, "   "); w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSearchItems_AdminOnlySearchesOwnItems(t *testing.T) {
	f := &searchFakes{embedder: &fakeQueryEmbedder{vec: []float32{1}}, candidates: []search.Candidate{{ItemID: 1, Vector: []float32{1}}}, items: map[int]models.Item{1: {ID: 1}}}
	r := setupSearch(t, f, models.AdminRole, 1)

	getSearch(r, "x")

	if f.gotUser != 1 {
		t.Errorf("user = %d, want 1 (admin's own)", f.gotUser)
	}
}

func TestSearchItems_IncludesLiteralTextMatchesFirst(t *testing.T) {
	// "luvas" tem nota semântica baixa para "Luvas de Boxe", mas o nome contém a palavra.
	f := &searchFakes{
		embedder: &fakeQueryEmbedder{vec: []float32{1, 0}},
		candidates: []search.Candidate{
			{ItemID: 1, Vector: []float32{0.15, 0.99}}, // ~0.15 → fora do ranking semântico
			{ItemID: 2, Vector: []float32{1, 0}},       // 1.0
		},
		items:       map[int]models.Item{1: {ID: 1}, 2: {ID: 2}},
		textResults: []models.Item{{ID: 1}},
	}
	r := setupSearch(t, f, models.UserRole, 7)

	_, resp := getSearch(r, "luvas")

	if resp.Mode != "semantic" || resultIDs(resp) != "[1 2]" {
		t.Fatalf("mode = %q ids = %s, want semantic [1 2]", resp.Mode, resultIDs(resp))
	}
	if resp.Results[0].Score == nil || *resp.Results[0].Score > 0.2 {
		t.Errorf("literal match score = %v, want its own (low) cosine", resp.Results[0].Score)
	}
	if resp.Results[0].Match != "text" || resp.Results[1].Match != "semantic" {
		t.Errorf("match = %q, %q; want text, semantic", resp.Results[0].Match, resp.Results[1].Match)
	}
	if f.gotTextQuery != "luvas" {
		t.Errorf("text query = %q", f.gotTextQuery)
	}
}

func TestSearchItems_DoesNotDuplicateItemsFoundByBoth(t *testing.T) {
	f := &searchFakes{
		embedder: &fakeQueryEmbedder{vec: []float32{1, 0}},
		candidates: []search.Candidate{
			{ItemID: 2, Vector: []float32{1, 0}},
			{ItemID: 3, Vector: []float32{1, 0.1}},
		},
		items:       map[int]models.Item{2: {ID: 2}, 3: {ID: 3}},
		textResults: []models.Item{{ID: 2}},
	}
	r := setupSearch(t, f, models.UserRole, 7)

	_, resp := getSearch(r, "moeda")

	if resultIDs(resp) != "[2 3]" {
		t.Errorf("ids = %s, want [2 3]", resultIDs(resp))
	}
}
