package search

import (
	"fmt"
	"math"
	"testing"

	"collection-manager-backend/internal/models"
)

func TestItemText_IncludesAllPartsAndSkipsEmpty(t *testing.T) {
	desc := "Moeda prateada"
	item := models.Item{
		Name: "Moeda 1 real", Description: &desc, Tags: models.TagList{"moeda", "1998"},
		Collection: models.Collection{Name: "Moedas", Category: models.Category{Name: "Numismática"}},
	}
	want := "Moeda 1 real. Moeda prateada. Tags: moeda, 1998. Coleção: Moedas. Categoria: Numismática."
	if got := ItemText(item); got != want {
		t.Errorf("ItemText = %q\nwant      %q", got, want)
	}

	if got := ItemText(models.Item{Name: "Selo"}); got != "Selo." {
		t.Errorf("ItemText minimal = %q, want %q", got, "Selo.")
	}
}

func TestCosine(t *testing.T) {
	near := func(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-6 }
	if c := Cosine([]float32{1, 2, 3}, []float32{2, 4, 6}); !near(c, 1) {
		t.Errorf("parallel = %v, want 1", c)
	}
	if c := Cosine([]float32{1, 0}, []float32{0, 1}); !near(c, 0) {
		t.Errorf("orthogonal = %v, want 0", c)
	}
	if c := Cosine([]float32{1, 0}, []float32{-1, 0}); !near(c, -1) {
		t.Errorf("opposite = %v, want -1", c)
	}
	if c := Cosine([]float32{1, 2}, []float32{1, 2, 3}); c != 0 {
		t.Errorf("different sizes = %v, want 0", c)
	}
	if c := Cosine([]float32{0, 0}, []float32{1, 1}); c != 0 {
		t.Errorf("zero norm = %v, want 0", c)
	}
}

func TestRank_OrdersFiltersAndLimits(t *testing.T) {
	q := []float32{1, 0}
	cands := []Candidate{
		{ItemID: 1, Vector: []float32{0, 1}},    // 0
		{ItemID: 2, Vector: []float32{1, 0}},    // 1
		{ItemID: 3, Vector: []float32{1, 1}},    // ~0.707
		{ItemID: 4, Vector: []float32{1, 0.1}},  // ~0.995
		{ItemID: 5, Vector: []float32{1, 2, 3}}, // tamanho diferente → 0
	}

	got := Rank(q, cands, 0.5, 2)
	if fmt.Sprint(ids(got)) != "[2 4]" {
		t.Errorf("Rank ids = %v, want [2 4]", ids(got))
	}

	all := Rank(q, cands, 0.5, 10)
	if fmt.Sprint(ids(all)) != "[2 4 3]" {
		t.Errorf("Rank ids = %v, want [2 4 3]", ids(all))
	}
	if all[0].Score < all[1].Score || all[1].Score < all[2].Score {
		t.Errorf("not sorted: %+v", all)
	}
}

func TestRank_EmptyCandidates(t *testing.T) {
	if got := Rank([]float32{1}, nil, MinScore, Limit); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestTopTags_CountsThenAlphabetical(t *testing.T) {
	lists := [][]string{{"moeda", "prata"}, {"moeda", "brasil"}, {"prata", "moeda"}, {"azul"}}
	if got := TopTags(lists, 3); fmt.Sprint(got) != "[moeda prata azul]" {
		t.Errorf("TopTags = %v, want [moeda prata azul]", got)
	}
	if got := TopTags(nil, 5); len(got) != 0 {
		t.Errorf("TopTags(nil) = %v", got)
	}
}

func ids(s []Scored) []int {
	out := make([]int, len(s))
	for i, x := range s {
		out[i] = x.ItemID
	}
	return out
}
