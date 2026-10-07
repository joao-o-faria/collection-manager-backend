package models

type Item struct {
	ID             int           `json:"id" gorm:"primaryKey;autoIncrement"`
	Name           string        `json:"name" gorm:"not null"`
	Description    *string       `json:"description,omitempty" gorm:"type:text"`
	Tags           TagList       `json:"tags" gorm:"type:text"`
	Price          float64       `json:"price" gorm:"type:numeric(10,2);not null;default:0"`
	CollectionID   int           `json:"collection_id" gorm:"not null"`
	Collection     Collection    `json:"collection,omitempty" gorm:"foreignKey:CollectionID"`
	UserID         uint          `json:"user_id" gorm:"not null;index"`
	BinaryObjectID *int          `json:"binary_object_id"`
	BinaryObject   *BinaryObject `json:"binary_object,omitempty" gorm:"foreignKey:BinaryObjectID"`
	Embedding      Vector        `json:"-" gorm:"type:real[]"`
}
