package storage

import (
	"context"
	"errors"
	"strings"

	"collection-manager-backend/internal/models"
	"collection-manager-backend/internal/search"
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

// ItemEmbeddings devolve os vetores dos itens do usuário que já foram indexados.
func ItemEmbeddings(ctx context.Context, userID uint) ([]search.Candidate, error) {
	if itemDB == nil {
		return nil, errors.New("conexão com o banco não inicializada")
	}
	// Lido linha a linha: o Scan do GORM numa struct anônima não chama o Scanner de
	// models.Vector e devolve vetores vazios.
	rows, err := itemDB.WithContext(ctx).Model(&models.Item{}).
		Select("id, embedding").
		Where("user_id = ? AND embedding IS NOT NULL", userID).
		Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []search.Candidate
	for rows.Next() {
		var id int
		var vec models.Vector
		if err := rows.Scan(&id, &vec); err != nil {
			return nil, err
		}
		out = append(out, search.Candidate{ItemID: id, Vector: vec})
	}
	return out, rows.Err()
}

// GetItemsByIDs carrega os itens do usuário na ordem dos ids informados (ids ausentes são ignorados).
func GetItemsByIDs(ctx context.Context, userID uint, ids []int) ([]models.Item, error) {
	if itemDB == nil {
		return nil, errors.New("conexão com o banco não inicializada")
	}
	if len(ids) == 0 {
		return []models.Item{}, nil
	}
	var items []models.Item
	err := itemDB.WithContext(ctx).
		Preload("Collection.Category").
		Preload("BinaryObject").
		Where("user_id = ? AND id IN ?", userID, ids).
		Find(&items).Error
	if err != nil {
		return nil, err
	}
	byID := make(map[int]models.Item, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	out := make([]models.Item, 0, len(ids))
	for _, id := range ids {
		if item, ok := byID[id]; ok {
			out = append(out, item)
		}
	}
	return out, nil
}

// SearchItemsText busca por texto (ILIKE) em nome, descrição e tags.
func SearchItemsText(ctx context.Context, userID uint, q string, limit int) ([]models.Item, error) {
	if itemDB == nil {
		return nil, errors.New("conexão com o banco não inicializada")
	}
	pattern := "%" + escapeLikePattern(strings.TrimSpace(q)) + "%"
	var items []models.Item
	err := itemDB.WithContext(ctx).
		Preload("Collection.Category").
		Preload("BinaryObject").
		Where("user_id = ?", userID).
		Where("name ILIKE ? OR description ILIKE ? OR tags ILIKE ?", pattern, pattern, pattern).
		Order("id DESC").
		Limit(limit).
		Find(&items).Error
	return items, err
}

type HomeTotals struct {
	Items       int64   `json:"items"`
	Collections int64   `json:"collections"`
	Categories  int64   `json:"categories"`
	TotalValue  float64 `json:"total_value"`
}

func GetHomeTotals(ctx context.Context, userID uint) (HomeTotals, error) {
	if itemDB == nil {
		return HomeTotals{}, errors.New("conexão com o banco não inicializada")
	}
	var t HomeTotals
	db := itemDB.WithContext(ctx)
	if err := db.Model(&models.Item{}).Where("user_id = ?", userID).Count(&t.Items).Error; err != nil {
		return t, err
	}
	if err := db.Model(&models.Collection{}).Where("user_id = ?", userID).Count(&t.Collections).Error; err != nil {
		return t, err
	}
	if err := db.Model(&models.Category{}).Where("user_id = ?", userID).Count(&t.Categories).Error; err != nil {
		return t, err
	}
	err := db.Model(&models.Item{}).Where("user_id = ?", userID).
		Select("COALESCE(SUM(price), 0)").Row().Scan(&t.TotalValue)
	return t, err
}

func GetRecentItems(ctx context.Context, userID uint, limit int) ([]models.Item, error) {
	if itemDB == nil {
		return nil, errors.New("conexão com o banco não inicializada")
	}
	var items []models.Item
	err := itemDB.WithContext(ctx).
		Preload("Collection.Category").
		Preload("BinaryObject").
		Where("user_id = ?", userID).
		Order("id DESC").
		Limit(limit).
		Find(&items).Error
	return items, err
}

func GetItemTags(ctx context.Context, userID uint) ([][]string, error) {
	if itemDB == nil {
		return nil, errors.New("conexão com o banco não inicializada")
	}
	var lists []models.TagList
	err := itemDB.WithContext(ctx).Model(&models.Item{}).
		Where("user_id = ? AND tags IS NOT NULL", userID).
		Pluck("tags", &lists).Error
	out := make([][]string, len(lists))
	for i, l := range lists {
		out[i] = l
	}
	return out, err
}
