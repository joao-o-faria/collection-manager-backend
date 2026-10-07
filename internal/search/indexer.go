package search

import (
	"context"
	"log"
	"time"

	"collection-manager-backend/internal/models"
)

type DocumentEmbedder interface {
	EmbedDocument(ctx context.Context, text string) ([]float32, error)
}

type IndexStore interface {
	GetItemForIndex(ctx context.Context, id int) (models.Item, error)
	SetItemEmbedding(ctx context.Context, id int, v models.Vector) error
	ItemsMissingEmbedding(ctx context.Context) ([]int, error)
}

// Indexer gera e grava o embedding de cada item.
type Indexer struct {
	embedder DocumentEmbedder
	store    IndexStore
}

func NewIndexer(e DocumentEmbedder, s IndexStore) *Indexer {
	return &Indexer{embedder: e, store: s}
}

func (ix *Indexer) IndexItem(ctx context.Context, id int) error {
	item, err := ix.store.GetItemForIndex(ctx, id)
	if err != nil {
		return err
	}
	vec, err := ix.embedder.EmbedDocument(ctx, ItemText(item))
	if err != nil {
		return err
	}
	return ix.store.SetItemEmbedding(ctx, id, models.Vector(vec))
}

// IndexItemAsync indexa em segundo plano; falhas só vão para o log (o item fica sem vetor).
func (ix *Indexer) IndexItemAsync(id int) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := ix.IndexItem(ctx, id); err != nil {
			log.Printf("indexação do item %d falhou: %v", id, err)
		}
	}()
}

// IndexMissing gera vetores para todos os itens sem embedding, um por vez.
func (ix *Indexer) IndexMissing(ctx context.Context) (int, error) {
	ids, err := ix.store.ItemsMissingEmbedding(ctx)
	if err != nil {
		return 0, err
	}
	indexed := 0
	for _, id := range ids {
		if err := ix.IndexItem(ctx, id); err != nil {
			log.Printf("indexação do item %d falhou: %v", id, err)
			continue
		}
		indexed++
	}
	return indexed, nil
}
