package storage

import (
	"context"
	"errors"

	"collection-manager-backend/internal/models"

	"gorm.io/gorm"
)

var itemDB *gorm.DB

func InitItemStorage(db *gorm.DB) error {
	itemDB = db

	return itemDB.AutoMigrate(&models.Item{})
}

func AddItem(ctx context.Context, userID uint, isAdmin bool, name string, description *string, tags []string, price float64, collectionID int, bin *BinaryObjectPayload) (models.Item, error) {
	if itemDB == nil {
		return models.Item{}, errors.New("conexão com o banco não inicializada")
	}

	owns, err := CollectionAccessible(ctx, userID, isAdmin, collectionID)
	if err != nil {
		return models.Item{}, err
	}
	if !owns {
		return models.Item{}, ErrNotFound
	}

	item := models.Item{
		Name:         name,
		Description:  description,
		Tags:         models.TagList(tags),
		Price:        price,
		CollectionID: collectionID,
		UserID:       userID,
	}

	if bin != nil {
		saved, err := AddBinaryObject(ctx, bin.Base64, bin.Filename, bin.Extension)
		if err != nil {
			return models.Item{}, err
		}
		item.BinaryObjectID = &saved.ID
	}

	if err := itemDB.WithContext(ctx).Create(&item).Error; err != nil {
		return models.Item{}, err
	}

	if err := itemDB.WithContext(ctx).
		Preload("BinaryObject").
		First(&item, item.ID).Error; err != nil {
		return item, err
	}

	return item, nil
}

func GetItemsByCollection(ctx context.Context, userID uint, isAdmin bool, collectionID int) ([]models.Item, error) {
	if itemDB == nil {
		return nil, errors.New("conexão com o banco não inicializada")
	}

	owns, err := CollectionAccessible(ctx, userID, isAdmin, collectionID)
	if err != nil {
		return nil, err
	}
	if !owns {
		return nil, ErrNotFound
	}

	var items []models.Item

	tx := itemDB.WithContext(ctx).
		Preload("BinaryObject").
		Where("collection_id = ?", collectionID)
	tx = scopeByUser(tx, userID, isAdmin)
	if err := tx.Order("id ASC").Find(&items).Error; err != nil {
		return nil, err
	}

	return items, nil
}

func UpdateItem(ctx context.Context, userID uint, isAdmin bool, id int, name string, description *string, tags []string, price float64, collectionID int, bin *BinaryObjectPayload) (models.Item, error) {
	if itemDB == nil {
		return models.Item{}, errors.New("conexão com o banco não inicializada")
	}

	var item models.Item
	tx := scopeByUser(itemDB.WithContext(ctx), userID, isAdmin)
	if err := tx.Where("id = ?", id).First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Item{}, ErrNotFound
		}
		return models.Item{}, err
	}

	owns, err := CollectionAccessible(ctx, userID, isAdmin, collectionID)
	if err != nil {
		return models.Item{}, err
	}
	if !owns {
		return models.Item{}, ErrNotFound
	}

	item.Name = name
	item.Description = description
	item.Tags = models.TagList(tags)
	item.Price = price
	item.CollectionID = collectionID

	if bin != nil {
		if item.BinaryObjectID != nil {
			updated, err := UpdateBinaryObject(ctx, *item.BinaryObjectID, bin.Base64, bin.Filename, bin.Extension)
			if err != nil {
				return models.Item{}, err
			}
			item.BinaryObjectID = &updated.ID
		} else {
			saved, err := AddBinaryObject(ctx, bin.Base64, bin.Filename, bin.Extension)
			if err != nil {
				return models.Item{}, err
			}
			item.BinaryObjectID = &saved.ID
		}
	}

	// O texto pode ter mudado: limpa o vetor para ele ser refeito (reindexação ao salvar
	// ou IndexMissing). Sem isso, com a IA fora, o vetor antigo ficaria para sempre.
	item.Embedding = nil

	if err := itemDB.WithContext(ctx).Save(&item).Error; err != nil {
		return models.Item{}, err
	}

	if err := itemDB.WithContext(ctx).
		Preload("BinaryObject").
		First(&item, item.ID).Error; err != nil {
		return item, err
	}

	return item, nil
}

func DeleteItem(ctx context.Context, userID uint, isAdmin bool, id int) error {
	if itemDB == nil {
		return errors.New("conexão com o banco não inicializada")
	}

	var item models.Item
	tx := scopeByUser(itemDB.WithContext(ctx), userID, isAdmin)
	if err := tx.Where("id = ?", id).First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}

	binID := item.BinaryObjectID

	if err := itemDB.WithContext(ctx).Delete(&item).Error; err != nil {
		return err
	}

	if binID != nil {
		if err := DeleteBinaryObject(ctx, *binID); err != nil {
			return err
		}
	}

	return nil
}
