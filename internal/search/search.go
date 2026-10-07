package search

import (
	"math"
	"sort"
	"strings"

	"collection-manager-backend/internal/models"
)

const (
	// MinScore é a similaridade mínima para um item aparecer na busca semântica.
	MinScore float32 = 0.35
	// Limit é o número máximo de resultados.
	Limit = 12
)

type Candidate struct {
	ItemID int
	Vector []float32
}

type Scored struct {
	ItemID int
	Score  float32
}

// ItemText monta o texto que representa o item para o embedding.
func ItemText(item models.Item) string {
	parts := []string{strings.TrimSpace(item.Name)}
	if item.Description != nil {
		parts = append(parts, strings.TrimSpace(*item.Description))
	}
	if len(item.Tags) > 0 {
		parts = append(parts, "Tags: "+strings.Join(item.Tags, ", "))
	}
	if item.Collection.Name != "" {
		parts = append(parts, "Coleção: "+item.Collection.Name)
	}
	if item.Collection.Category.Name != "" {
		parts = append(parts, "Categoria: "+item.Collection.Category.Name)
	}

	nonEmpty := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSuffix(p, "."); p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, ". ") + "."
}

// Cosine é a similaridade de cosseno entre dois vetores (0 se incompatíveis).
func Cosine(a, b []float32) float32 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

// Rank ordena os candidatos pela similaridade com a consulta, descartando os abaixo de minScore.
func Rank(query []float32, candidates []Candidate, minScore float32, limit int) []Scored {
	out := make([]Scored, 0, len(candidates))
	for _, c := range candidates {
		if s := Cosine(query, c.Vector); s >= minScore {
			out = append(out, Scored{ItemID: c.ItemID, Score: s})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// TopTags devolve as n tags mais frequentes (empates em ordem alfabética).
func TopTags(lists [][]string, n int) []string {
	counts := map[string]int{}
	for _, tags := range lists {
		for _, t := range tags {
			if t = strings.TrimSpace(t); t != "" {
				counts[t]++
			}
		}
	}
	tags := make([]string, 0, len(counts))
	for t := range counts {
		tags = append(tags, t)
	}
	sort.Slice(tags, func(i, j int) bool {
		if counts[tags[i]] != counts[tags[j]] {
			return counts[tags[i]] > counts[tags[j]]
		}
		return tags[i] < tags[j]
	})
	if len(tags) > n {
		tags = tags[:n]
	}
	return tags
}
