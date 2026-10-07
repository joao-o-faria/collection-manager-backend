package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"collection-manager-backend/internal/ai"
	"collection-manager-backend/internal/models"

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
