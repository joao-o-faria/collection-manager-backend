package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"collection-manager-backend/internal/models"
	"collection-manager-backend/internal/storage"

	"github.com/gin-gonic/gin"
)

func setupHome(t *testing.T, totalsErr error) (*gin.Engine, *[]uint) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var users []uint

	prevT, prevR, prevG := loadHomeTotals, loadRecentItems, loadItemTags
	loadHomeTotals = func(_ context.Context, uid uint) (storage.HomeTotals, error) {
		users = append(users, uid)
		return storage.HomeTotals{Items: 3, Collections: 2, Categories: 1, TotalValue: 42.5}, totalsErr
	}
	loadRecentItems = func(_ context.Context, uid uint, limit int) ([]models.Item, error) {
		users = append(users, uid)
		if limit != 6 {
			t.Errorf("limit = %d, want 6", limit)
		}
		return []models.Item{{ID: 3}, {ID: 2}}, nil
	}
	loadItemTags = func(_ context.Context, uid uint) ([][]string, error) {
		users = append(users, uid)
		return [][]string{{"moeda", "prata"}, {"moeda"}}, nil
	}
	t.Cleanup(func() { loadHomeTotals, loadRecentItems, loadItemTags = prevT, prevR, prevG })

	r := gin.New()
	r.GET("/home/summary", func(c *gin.Context) {
		c.Set("user_id", uint(1))
		c.Set("user_role", models.AdminRole)
		GetHomeSummary(c)
	})
	return r, &users
}

func TestGetHomeSummary_ReturnsTotalsRecentAndTopTags(t *testing.T) {
	r, users := setupHome(t, nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/home/summary", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	var resp HomeSummaryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Totals != (storage.HomeTotals{Items: 3, Collections: 2, Categories: 1, TotalValue: 42.5}) {
		t.Errorf("totals = %+v", resp.Totals)
	}
	if len(resp.RecentItems) != 2 || resp.RecentItems[0].ID != 3 {
		t.Errorf("recent = %+v", resp.RecentItems)
	}
	if fmt.Sprint(resp.TopTags) != "[moeda prata]" {
		t.Errorf("top tags = %v", resp.TopTags)
	}
	for _, u := range *users {
		if u != 1 {
			t.Errorf("queried user %d, want only 1 (admin's own data)", u)
		}
	}
}

func TestGetHomeSummary_ErrorReturns500(t *testing.T) {
	r, _ := setupHome(t, errors.New("db down"))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/home/summary", nil))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}
