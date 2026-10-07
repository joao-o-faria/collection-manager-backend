package storage

import (
	"context"
	"errors"

	"collection-manager-backend/internal/models"

	"gorm.io/gorm"
)

var ErrCategoryMismatch = errors.New("coleção não pertence à categoria informada")

// RefInput referencia uma entidade existente (ID > 0) ou pede a criação de uma nova (NewName).
type RefInput struct {
	ID      int
	NewName string
}

type QuickAddInput struct {
	Category    RefInput
	Collection  RefInput
	Name        string
	Description *string
	Tags        []string
	Price       float64
	Binary      *BinaryObjectPayload
}

// QuickAdd cria (se preciso) categoria e coleção e cadastra o item, tudo numa transação.
func QuickAdd(ctx context.Context, userID uint, isAdmin bool, in QuickAddInput) (models.Item, error) {
	if itemDB == nil {
		return models.Item{}, errors.New("conexão com o banco não inicializada")
	}

	var item models.Item
	err := itemDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var category models.Category
		if in.Category.ID > 0 {
			if err := scopeByUser(tx, userID, isAdmin).First(&category, in.Category.ID).Error; err != nil {
				return notFoundOr(err)
			}
		} else {
			category = models.Category{Name: in.Category.NewName, UserID: userID}
			if err := tx.Create(&category).Error; err != nil {
				return err
			}
		}

		var collection models.Collection
		if in.Collection.ID > 0 {
			if err := scopeByUser(tx, userID, isAdmin).First(&collection, in.Collection.ID).Error; err != nil {
				return notFoundOr(err)
			}
			if collection.CategoryID != category.ID {
				return ErrCategoryMismatch
			}
		} else {
			collection = models.Collection{Name: in.Collection.NewName, CategoryID: category.ID, UserID: userID}
			if err := tx.Create(&collection).Error; err != nil {
				return err
			}
		}

		item = models.Item{
			Name:         in.Name,
			Description:  in.Description,
			Tags:         models.TagList(in.Tags),
			Price:        in.Price,
			CollectionID: collection.ID,
			UserID:       userID,
		}
		if in.Binary != nil {
			bin := models.BinaryObject{Base64: in.Binary.Base64, Filename: in.Binary.Filename, Extension: in.Binary.Extension}
			if err := tx.Create(&bin).Error; err != nil {
				return err
			}
			item.BinaryObjectID = &bin.ID
		}
		return tx.Create(&item).Error
	})
	if err != nil {
		return models.Item{}, err
	}

	if err := itemDB.WithContext(ctx).Preload("BinaryObject").First(&item, item.ID).Error; err != nil {
		return item, err
	}
	return item, nil
}

func notFoundOr(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}
