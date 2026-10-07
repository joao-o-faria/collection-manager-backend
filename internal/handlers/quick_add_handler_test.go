package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"collection-manager-backend/internal/ai"
	"collection-manager-backend/internal/models"
	"collection-manager-backend/internal/storage"

	"github.com/gin-gonic/gin"
)

type fakeAnalyzer struct {
	gotImage string
	gotCats  []ai.CategoryOption
	gotCols  []ai.CollectionOption
	result   ai.ImageAnalysis
	err      error
}

func (f *fakeAnalyzer) AnalyzeItemImage(_ context.Context, img string, cats []ai.CategoryOption, cols []ai.CollectionOption) (ai.ImageAnalysis, error) {
	f.gotImage, f.gotCats, f.gotCols = img, cats, cols
	return f.result, f.err
}

func newQuickAddRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	withActor := func(h gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			c.Set("user_id", uint(7))
			c.Set("user_role", models.UserRole)
			h(c)
		}
	}
	r.POST("/quick-add/analyze", withActor(AnalyzeQuickAdd))
	r.POST("/quick-add", withActor(CreateQuickAdd))
	return r
}

func setupAnalyze(t *testing.T, a imageAnalyzer) *gin.Engine {
	t.Helper()
	prevA, prevCats, prevCols := analyzer, listCategories, listCollections
	analyzer = a
	listCategories = func(_ context.Context, userID uint, isAdmin bool, _ string) ([]models.Category, error) {
		return testCats, nil
	}
	listCollections = func(_ context.Context, userID uint, isAdmin bool, _ string) ([]models.Collection, error) {
		return testCols, nil
	}
	t.Cleanup(func() { analyzer, listCategories, listCollections = prevA, prevCats, prevCols })
	return newQuickAddRouter(t)
}

func post(r *gin.Engine, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestAnalyzeQuickAdd_ReturnsResolvedAnalysis(t *testing.T) {
	fake := &fakeAnalyzer{result: ai.ImageAnalysis{
		Name: "Moeda", Description: "d", Tags: []string{"moeda"},
		Category: ai.RefSuggestion{ID: 1}, Collection: ai.RefSuggestion{ID: 99, NewName: "Moedas de prata"},
	}}
	r := setupAnalyze(t, fake)

	w := post(r, "/quick-add/analyze", `{"image_base64":"aW1n"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	var resp QuickAddAnalysisResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Category != (ResolvedRef{ID: 1, Name: "Numismática"}) || resp.Collection != (ResolvedRef{Name: "Moedas de prata", IsNew: true}) {
		t.Errorf("resp = %+v", resp)
	}
	if fake.gotImage != "aW1n" {
		t.Errorf("image = %q", fake.gotImage)
	}
	if len(fake.gotCats) != 2 || fake.gotCats[0] != (ai.CategoryOption{ID: 1, Name: "Numismática"}) {
		t.Errorf("cats = %+v", fake.gotCats)
	}
	if len(fake.gotCols) != 2 || fake.gotCols[1] != (ai.CollectionOption{ID: 20, Name: "Hot Wheels", CategoryID: 2}) {
		t.Errorf("cols = %+v", fake.gotCols)
	}
}

func TestAnalyzeQuickAdd_RejectsMissingImage(t *testing.T) {
	r := setupAnalyze(t, &fakeAnalyzer{})

	if w := post(r, "/quick-add/analyze", `{"image_base64":"  "}`); w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAnalyzeQuickAdd_ServiceUnavailableWhenAIDown(t *testing.T) {
	r := setupAnalyze(t, &fakeAnalyzer{err: ai.ErrUnavailable})

	if w := post(r, "/quick-add/analyze", `{"image_base64":"aW1n"}`); w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func setupCreate(t *testing.T, fn func(context.Context, uint, bool, storage.QuickAddInput) (models.Item, error)) *gin.Engine {
	t.Helper()
	prev := quickAdd
	quickAdd = fn
	t.Cleanup(func() { quickAdd = prev })
	return newQuickAddRouter(t)
}

const validCreateBody = `{
  "category": {"new_name": " Selos "},
  "collection": {"new_name": "Selos raros"},
  "item": {"name": " Selo azul ", "description": " d ", "tags": ["selo", "selo"], "price": 12.5,
           "binary_object": {"base64": "aW1n", "filename": "a.jpg", "extension": "jpg"}}
}`

func TestCreateQuickAdd_PassesNormalizedInputAndReturns201(t *testing.T) {
	var got storage.QuickAddInput
	r := setupCreate(t, func(_ context.Context, userID uint, isAdmin bool, in storage.QuickAddInput) (models.Item, error) {
		got = in
		return models.Item{ID: 5, Name: in.Name, CollectionID: 42}, nil
	})

	w := post(r, "/quick-add", validCreateBody)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	if got.Category != (storage.RefInput{NewName: "Selos"}) || got.Collection != (storage.RefInput{NewName: "Selos raros"}) {
		t.Errorf("refs = %+v / %+v", got.Category, got.Collection)
	}
	if got.Name != "Selo azul" || got.Description == nil || *got.Description != "d" || len(got.Tags) != 1 || got.Price != 12.5 {
		t.Errorf("item = %+v", got)
	}
	if got.Binary == nil || got.Binary.Base64 != "aW1n" {
		t.Errorf("binary = %+v", got.Binary)
	}
	var item models.Item
	_ = json.Unmarshal(w.Body.Bytes(), &item)
	if item.CollectionID != 42 {
		t.Errorf("collection_id = %d", item.CollectionID)
	}
}

func TestCreateQuickAdd_AcceptsExistingIDs(t *testing.T) {
	var got storage.QuickAddInput
	r := setupCreate(t, func(_ context.Context, _ uint, _ bool, in storage.QuickAddInput) (models.Item, error) {
		got = in
		return models.Item{ID: 1}, nil
	})

	w := post(r, "/quick-add", `{"category":{"id":1},"collection":{"id":10},"item":{"name":"x","price":0}}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	if got.Category != (storage.RefInput{ID: 1}) || got.Collection != (storage.RefInput{ID: 10}) {
		t.Errorf("refs = %+v / %+v", got.Category, got.Collection)
	}
}

func TestCreateQuickAdd_ValidationErrors(t *testing.T) {
	called := false
	r := setupCreate(t, func(context.Context, uint, bool, storage.QuickAddInput) (models.Item, error) {
		called = true
		return models.Item{}, nil
	})

	cases := map[string]string{
		"ref with id and name":               `{"category":{"id":1,"new_name":"a"},"collection":{"id":10},"item":{"name":"x"}}`,
		"ref with neither":                   `{"category":{},"collection":{"id":10},"item":{"name":"x"}}`,
		"blank new name":                     `{"category":{"new_name":"  "},"collection":{"new_name":"b"},"item":{"name":"x"}}`,
		"new category + existing collection": `{"category":{"new_name":"a"},"collection":{"id":10},"item":{"name":"x"}}`,
		"blank item name":                    `{"category":{"id":1},"collection":{"id":10},"item":{"name":"  "}}`,
		"negative price":                     `{"category":{"id":1},"collection":{"id":10},"item":{"name":"x","price":-1}}`,
	}
	for name, body := range cases {
		if w := post(r, "/quick-add", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, w.Code)
		}
	}
	if called {
		t.Error("storage.QuickAdd should not be called on invalid input")
	}
}

func TestCreateQuickAdd_MapsStorageErrors(t *testing.T) {
	cases := map[error]int{
		storage.ErrNotFound:         http.StatusNotFound,
		storage.ErrCategoryMismatch: http.StatusBadRequest,
		errors.New("db down"):       http.StatusInternalServerError,
	}
	for storageErr, want := range cases {
		r := setupCreate(t, func(context.Context, uint, bool, storage.QuickAddInput) (models.Item, error) {
			return models.Item{}, storageErr
		})
		w := post(r, "/quick-add", `{"category":{"id":1},"collection":{"id":10},"item":{"name":"x"}}`)
		if w.Code != want {
			t.Errorf("%v: status = %d, want %d", storageErr, w.Code, want)
		}
	}
}

func newAdminQuickAddRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	withAdmin := func(h gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			c.Set("user_id", uint(1))
			c.Set("user_role", models.AdminRole)
			h(c)
		}
	}
	r.POST("/quick-add/analyze", withAdmin(AnalyzeQuickAdd))
	r.POST("/quick-add", withAdmin(CreateQuickAdd))
	return r
}

func TestAnalyzeQuickAdd_AdminOnlySeesOwnData(t *testing.T) {
	setupAnalyze(t, &fakeAnalyzer{result: ai.ImageAnalysis{Name: "x"}})
	var catAdmin, colAdmin []bool
	listCategories = func(_ context.Context, _ uint, isAdmin bool, _ string) ([]models.Category, error) {
		catAdmin = append(catAdmin, isAdmin)
		return testCats, nil
	}
	listCollections = func(_ context.Context, _ uint, isAdmin bool, _ string) ([]models.Collection, error) {
		colAdmin = append(colAdmin, isAdmin)
		return testCols, nil
	}

	w := post(newAdminQuickAddRouter(), "/quick-add/analyze", `{"image_base64":"aW1n"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if len(catAdmin) != 1 || catAdmin[0] || len(colAdmin) != 1 || colAdmin[0] {
		t.Errorf("lists called with isAdmin cats=%v cols=%v, want [false]", catAdmin, colAdmin)
	}
}

func TestCreateQuickAdd_AdminOnlyUsesOwnData(t *testing.T) {
	var gotAdmin *bool
	setupCreate(t, func(_ context.Context, _ uint, isAdmin bool, _ storage.QuickAddInput) (models.Item, error) {
		gotAdmin = &isAdmin
		return models.Item{ID: 1}, nil
	})

	w := post(newAdminQuickAddRouter(), "/quick-add", `{"category":{"id":1},"collection":{"id":10},"item":{"name":"x"}}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d", w.Code)
	}
	if gotAdmin == nil || *gotAdmin {
		t.Errorf("QuickAdd isAdmin = %v, want false", gotAdmin)
	}
}
