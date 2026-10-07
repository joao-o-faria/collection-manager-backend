package storage

import (
	"context"
	"errors"

	"collection-manager-backend/internal/models"
)

// IndexStore expõe ao search.Indexer as consultas de indexação.
type IndexStore struct{}

func (IndexStore) GetItemForIndex(ctx context.Context, id int) (models.Item, error) {
	if itemDB == nil {
		return models.Item{}, errors.New("conexão com o banco não inicializada")
	}
	var item models.Item
	err := itemDB.WithContext(ctx).Preload("Collection.Category").First(&item, id).Error
	return item, notFoundOr(err)
}

func (IndexStore) SetItemEmbedding(ctx context.Context, id int, v models.Vector) error {
	if itemDB == nil {
		return errors.New("conexão com o banco não inicializada")
	}
	return itemDB.WithContext(ctx).Model(&models.Item{}).Where("id = ?", id).Update("embedding", v).Error
}

func (IndexStore) ItemsMissingEmbedding(ctx context.Context) ([]int, error) {
	if itemDB == nil {
		return nil, errors.New("conexão com o banco não inicializada")
	}
	var ids []int
	err := itemDB.WithContext(ctx).Model(&models.Item{}).Where("embedding IS NULL").Order("id ASC").Pluck("id", &ids).Error
	return ids, err
}
