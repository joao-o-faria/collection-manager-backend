package search

import (
	"context"
	"errors"
	"testing"

	"collection-manager-backend/internal/models"
)

type fakeEmbedder struct {
	texts []string
	fail  map[string]bool
}

func (f *fakeEmbedder) EmbedDocument(_ context.Context, text string) ([]float32, error) {
	f.texts = append(f.texts, text)
	if f.fail[text] {
		return nil, errors.New("ollama fora")
	}
	return []float32{float32(len(text))}, nil
}

type fakeStore struct {
	items   map[int]models.Item
	saved   map[int]models.Vector
	missing []int
}

func (s *fakeStore) GetItemForIndex(_ context.Context, id int) (models.Item, error) {
	item, ok := s.items[id]
	if !ok {
		return models.Item{}, errors.New("não encontrado")
	}
	return item, nil
}

func (s *fakeStore) SetItemEmbedding(_ context.Context, id int, v models.Vector) error {
	s.saved[id] = v
	return nil
}

func (s *fakeStore) ItemsMissingEmbedding(context.Context) ([]int, error) {
	return s.missing, nil
}

func newFakes() (*fakeEmbedder, *fakeStore) {
	return &fakeEmbedder{fail: map[string]bool{}}, &fakeStore{
		items: map[int]models.Item{1: {ID: 1, Name: "Moeda"}, 2: {ID: 2, Name: "Selo"}, 3: {ID: 3, Name: "Carro"}},
		saved: map[int]models.Vector{},
	}
}

func TestIndexItem_EmbedsItemTextAndSaves(t *testing.T) {
	emb, store := newFakes()

	if err := NewIndexer(emb, store).IndexItem(context.Background(), 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(emb.texts) != 1 || emb.texts[0] != "Moeda." {
		t.Errorf("embedded texts = %v", emb.texts)
	}
	if v, ok := store.saved[1]; !ok || len(v) != 1 {
		t.Errorf("saved = %v", store.saved)
	}
}

func TestIndexItem_DoesNotSaveWhenEmbedFails(t *testing.T) {
	emb, store := newFakes()
	emb.fail["Moeda."] = true

	if err := NewIndexer(emb, store).IndexItem(context.Background(), 1); err == nil {
		t.Error("want error")
	}
	if len(store.saved) != 0 {
		t.Errorf("saved = %v, want nothing", store.saved)
	}
}

func TestIndexMissing_ContinuesPastFailuresAndCounts(t *testing.T) {
	emb, store := newFakes()
	store.missing = []int{1, 2, 3}
	emb.fail["Selo."] = true

	n, err := NewIndexer(emb, store).IndexMissing(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 2 || len(store.saved) != 2 || store.saved[2] != nil {
		t.Errorf("indexed = %d, saved = %v", n, store.saved)
	}
}
