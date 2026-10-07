package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"collection-manager-backend/internal/ai"
	"collection-manager-backend/internal/models"
	"collection-manager-backend/internal/storage"

	"github.com/gin-gonic/gin"
)

type fakeSuggester struct {
	got    ai.ItemContext
	result ai.ItemSuggestion
	err    error
}

func (f *fakeSuggester) SuggestItemDetails(_ context.Context, item ai.ItemContext) (ai.ItemSuggestion, error) {
	f.got = item
	return f.result, f.err
}

func setupSuggest(t *testing.T, s itemSuggester, lookup func(context.Context, uint, bool, int) (models.Collection, error)) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	prevSuggester, prevLookup := suggester, lookupCollection
	suggester, lookupCollection = s, lookup
	t.Cleanup(func() { suggester, lookupCollection = prevSuggester, prevLookup })

	r := gin.New()
	r.POST("/items/suggest", func(c *gin.Context) {
		c.Set("user_id", uint(7))
		c.Set("user_role", models.UserRole)
		SuggestItemDetails(c)
	})
	return r
}

func postSuggest(r *gin.Engine, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/items/suggest", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func moedasCollection(_ context.Context, userID uint, isAdmin bool, id int) (models.Collection, error) {
	if userID != 7 || isAdmin || id != 3 {
		return models.Collection{}, storage.ErrNotFound
	}
	return models.Collection{ID: 3, Name: "Moedas", Category: models.Category{Name: "Numismática"}}, nil
}

func TestSuggestItemDetails_ReturnsSuggestion(t *testing.T) {
	fake := &fakeSuggester{result: ai.ItemSuggestion{Description: "Moeda antiga.", Tags: []string{"moeda", "1998"}}}
	r := setupSuggest(t, fake, moedasCollection)

	w := postSuggest(r, `{"name":"  Moeda 1 real ","collection_id":3,"image_base64":"aGVsbG8="}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	var resp ai.ItemSuggestion
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Description != "Moeda antiga." || len(resp.Tags) != 2 {
		t.Errorf("resp = %+v", resp)
	}
	want := ai.ItemContext{Name: "Moeda 1 real", Collection: "Moedas", Category: "Numismática", ImageBase64: "aGVsbG8="}
	if fake.got != want {
		t.Errorf("suggester got %+v, want %+v", fake.got, want)
	}
}

func TestSuggestItemDetails_NormalizesTags(t *testing.T) {
	fake := &fakeSuggester{result: ai.ItemSuggestion{Description: "x", Tags: []string{" moeda ", "", "moeda", "prata"}}}
	r := setupSuggest(t, fake, moedasCollection)

	w := postSuggest(r, `{"name":"Moeda","collection_id":3}`)

	var resp ai.ItemSuggestion
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if fmt.Sprint(resp.Tags) != "[moeda prata]" {
		t.Errorf("tags = %v, want [moeda prata]", resp.Tags)
	}
}

func TestSuggestItemDetails_RejectsBlankName(t *testing.T) {
	r := setupSuggest(t, &fakeSuggester{}, moedasCollection)

	w := postSuggest(r, `{"name":"   ","collection_id":3}`)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSuggestItemDetails_NotFoundForInaccessibleCollection(t *testing.T) {
	r := setupSuggest(t, &fakeSuggester{}, moedasCollection)

	w := postSuggest(r, `{"name":"Moeda","collection_id":99}`)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestSuggestItemDetails_ServiceUnavailableWhenAIDown(t *testing.T) {
	r := setupSuggest(t, &fakeSuggester{err: ai.ErrUnavailable}, moedasCollection)

	w := postSuggest(r, `{"name":"Moeda","collection_id":3}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}
