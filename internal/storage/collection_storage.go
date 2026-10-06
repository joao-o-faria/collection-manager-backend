package storage

import (
	"context"
	"errors"
	"strings"

	"collection-manager-backend/internal/models"

	"gorm.io/gorm"
)

var collectionDB *gorm.DB

func InitCollectionStorage(db *gorm.DB) error {
	collectionDB = db

	return collectionDB.AutoMigrate(&models.Collection{})
}

type BinaryObjectPayload struct {
	Base64    string
	Filename  string
	Extension string
}

func AddCollection(ctx context.Context, userID uint, isAdmin bool, name string, categoryID int, bin *BinaryObjectPayload) (models.Collection, error) {
	if collectionDB == nil {
		return models.Collection{}, errors.New("conexão com o banco não inicializada")
	}

	owns, err := CategoryAccessible(ctx, userID, isAdmin, categoryID)
	if err != nil {
		return models.Collection{}, err
	}
	if !owns {
		return models.Collection{}, ErrNotFound
	}

	collection := models.Collection{
		Name:       name,
		CategoryID: categoryID,
		UserID:     userID,
	}

	if bin != nil {
		saved, err := AddBinaryObject(ctx, bin.Base64, bin.Filename, bin.Extension)
		if err != nil {
			return models.Collection{}, err
		}
		collection.BinaryObjectID = &saved.ID
	}

	if err := collectionDB.WithContext(ctx).Create(&collection).Error; err != nil {
		return models.Collection{}, err
	}

	if err := collectionDB.WithContext(ctx).
		Preload("Category").
		Preload("BinaryObject").
		First(&collection, collection.ID).Error; err != nil {
		return collection, err
	}

	return collection, nil
}

func GetCollections(ctx context.Context, userID uint, isAdmin bool, search string) ([]models.Collection, error) {
	if collectionDB == nil {
		return nil, errors.New("conexão com o banco não inicializada")
	}

	var collections []models.Collection

	tx := scopeByUser(collectionDB.WithContext(ctx), userID, isAdmin).
		Preload("Category").
		Preload("BinaryObject")
	if trimmed := strings.TrimSpace(search); trimmed != "" {
		pattern := "%" + escapeLikePattern(trimmed) + "%"
		tx = tx.Where("name ILIKE ?", pattern)
	}
	if err := tx.Order("id ASC").Find(&collections).Error; err != nil {
		return nil, err
	}

	return collections, nil
}

func GetCollection(ctx context.Context, userID uint, isAdmin bool, id int) (models.Collection, error) {
	if collectionDB == nil {
		return models.Collection{}, errors.New("conexão com o banco não inicializada")
	}

	var collection models.Collection
	err := scopeByUser(collectionDB.WithContext(ctx), userID, isAdmin).
		Preload("Category").
		First(&collection, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return collection, ErrNotFound
	}

	return collection, err
}

func UpdateCollection(ctx context.Context, userID uint, isAdmin bool, id int, name string, categoryID int, bin *BinaryObjectPayload) (models.Collection, error) {
	if collectionDB == nil {
		return models.Collection{}, errors.New("conexão com o banco não inicializada")
	}

	var collection models.Collection
	tx := scopeByUser(collectionDB.WithContext(ctx), userID, isAdmin)
	if err := tx.Where("id = ?", id).First(&collection).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Collection{}, ErrNotFound
		}
		return models.Collection{}, err
	}

	owns, err := CategoryAccessible(ctx, userID, isAdmin, categoryID)
	if err != nil {
		return models.Collection{}, err
	}
	if !owns {
		return models.Collection{}, ErrNotFound
	}

	collection.Name = name
	collection.CategoryID = categoryID

	if bin != nil {
		if collection.BinaryObjectID != nil {
			updated, err := UpdateBinaryObject(ctx, *collection.BinaryObjectID, bin.Base64, bin.Filename, bin.Extension)
			if err != nil {
				return models.Collection{}, err
			}
			collection.BinaryObjectID = &updated.ID
		} else {
			saved, err := AddBinaryObject(ctx, bin.Base64, bin.Filename, bin.Extension)
			if err != nil {
				return models.Collection{}, err
			}
			collection.BinaryObjectID = &saved.ID
		}
	}

	if err := collectionDB.WithContext(ctx).Save(&collection).Error; err != nil {
		return models.Collection{}, err
	}

	if err := collectionDB.WithContext(ctx).
		Preload("Category").
		Preload("BinaryObject").
		First(&collection, collection.ID).Error; err != nil {
		return collection, err
	}

	return collection, nil
}

func DeleteCollection(ctx context.Context, userID uint, isAdmin bool, id int) error {
	if collectionDB == nil {
		return errors.New("conexão com o banco não inicializada")
	}

	var collection models.Collection
	tx := scopeByUser(collectionDB.WithContext(ctx), userID, isAdmin)
	if err := tx.Where("id = ?", id).First(&collection).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}

	binID := collection.BinaryObjectID

	if err := collectionDB.WithContext(ctx).Delete(&collection).Error; err != nil {
		return err
	}

	if binID != nil {
		if err := DeleteBinaryObject(ctx, *binID); err != nil {
			return err
		}
	}

	return nil
}

func CollectionAccessible(ctx context.Context, userID uint, isAdmin bool, collectionID int) (bool, error) {
	if collectionDB == nil {
		return false, errors.New("conexão com o banco não inicializada")
	}

	var count int64
	tx := scopeByUser(collectionDB.WithContext(ctx).Model(&models.Collection{}), userID, isAdmin)
	if err := tx.Where("id = ?", collectionID).Count(&count).Error; err != nil {
		return false, err
	}

	return count > 0, nil
}
