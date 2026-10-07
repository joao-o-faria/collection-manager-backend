package handlers

import (
	"fmt"
	"testing"

	"collection-manager-backend/internal/ai"
	"collection-manager-backend/internal/models"
)

var (
	testCats = []models.Category{{ID: 1, Name: "Numismática"}, {ID: 2, Name: "Miniaturas"}}
	testCols = []models.Collection{
		{ID: 10, Name: "Moedas antigas", CategoryID: 1},
		{ID: 20, Name: "Hot Wheels", CategoryID: 2},
	}
)

func analysis(cat, col ai.RefSuggestion) ai.ImageAnalysis {
	return ai.ImageAnalysis{Name: "Moeda", Description: "desc", Tags: []string{"moeda"}, Category: cat, Collection: col}
}

func TestResolve_KeepsValidExistingRefs(t *testing.T) {
	r := resolveAnalysis(analysis(ai.RefSuggestion{ID: 1}, ai.RefSuggestion{ID: 10}), testCats, testCols)
	if r.Category != (ResolvedRef{ID: 1, Name: "Numismática"}) || r.Collection != (ResolvedRef{ID: 10, Name: "Moedas antigas"}) {
		t.Errorf("got %+v / %+v", r.Category, r.Collection)
	}
}

func TestResolve_KeepsNewRefs(t *testing.T) {
	r := resolveAnalysis(analysis(ai.RefSuggestion{NewName: " Selos "}, ai.RefSuggestion{NewName: "Selos raros"}), testCats, testCols)
	if r.Category != (ResolvedRef{Name: "Selos", IsNew: true}) || r.Collection != (ResolvedRef{Name: "Selos raros", IsNew: true}) {
		t.Errorf("got %+v / %+v", r.Category, r.Collection)
	}
}

func TestResolve_UnknownCategoryIDBecomesNew(t *testing.T) {
	r := resolveAnalysis(analysis(ai.RefSuggestion{ID: 99, NewName: "Selos"}, ai.RefSuggestion{NewName: "Selos raros"}), testCats, testCols)
	if r.Category != (ResolvedRef{Name: "Selos", IsNew: true}) {
		t.Errorf("category = %+v", r.Category)
	}
}

func TestResolve_UnknownCategoryWithoutNameBecomesGeral(t *testing.T) {
	r := resolveAnalysis(analysis(ai.RefSuggestion{ID: 99}, ai.RefSuggestion{}), testCats, testCols)
	if r.Category != (ResolvedRef{Name: "Geral", IsNew: true}) || r.Collection != (ResolvedRef{Name: "Geral", IsNew: true}) {
		t.Errorf("got %+v / %+v", r.Category, r.Collection)
	}
}

func TestResolve_UnknownCollectionIDBecomesNewNamedAfterCategory(t *testing.T) {
	r := resolveAnalysis(analysis(ai.RefSuggestion{ID: 1}, ai.RefSuggestion{ID: 99}), testCats, testCols)
	if r.Collection != (ResolvedRef{Name: "Numismática", IsNew: true}) {
		t.Errorf("collection = %+v", r.Collection)
	}
}

func TestResolve_ExistingCollectionOverridesMismatchedCategory(t *testing.T) {
	r := resolveAnalysis(analysis(ai.RefSuggestion{ID: 1}, ai.RefSuggestion{ID: 20}), testCats, testCols)
	if r.Category != (ResolvedRef{ID: 2, Name: "Miniaturas"}) || r.Collection != (ResolvedRef{ID: 20, Name: "Hot Wheels"}) {
		t.Errorf("got %+v / %+v", r.Category, r.Collection)
	}
}

func TestResolve_NewCategoryForcesNewCollection(t *testing.T) {
	r := resolveAnalysis(analysis(ai.RefSuggestion{NewName: "Selos"}, ai.RefSuggestion{ID: 10}), testCats, testCols)
	if r.Category != (ResolvedRef{Name: "Selos", IsNew: true}) || r.Collection != (ResolvedRef{Name: "Selos", IsNew: true}) {
		t.Errorf("got %+v / %+v", r.Category, r.Collection)
	}
}

func TestResolve_NormalizesItemFields(t *testing.T) {
	a := ai.ImageAnalysis{Name: "  ", Description: " d ", Tags: []string{" a ", "", "a"},
		Category: ai.RefSuggestion{ID: 1}, Collection: ai.RefSuggestion{ID: 10}}
	r := resolveAnalysis(a, testCats, testCols)
	if r.Name != "Item sem nome" || r.Description != "d" || fmt.Sprint(r.Tags) != "[a]" {
		t.Errorf("got %+v", r)
	}
}

func TestResolve_NilTagsBecomeEmptySlice(t *testing.T) {
	a := ai.ImageAnalysis{Name: "x", Category: ai.RefSuggestion{ID: 1}, Collection: ai.RefSuggestion{ID: 10}}
	if r := resolveAnalysis(a, testCats, testCols); r.Tags == nil {
		t.Error("tags = nil, want empty slice")
	}
}
