//go:build integration

package storage

import (
	"context"
	"os"
	"testing"

	"collection-manager-backend/internal/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Roda contra um Postgres real: go test -tags integration ./internal/storage/
// (usa DATABASE_URL ou o banco local do projeto).
func openIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://postgres:postgres@localhost:5432/collection_manager?sslmode=disable"
	}
	db, err := gorm.Open(postgres.Open(url), &gorm.Config{})
	if err != nil {
		t.Skipf("Postgres indisponível: %v", err)
	}
	categoryDB, collectionDB, itemDB, binaryObjectDB = db, db, db, db
	return db
}

func TestUpdateItem_ClearsEmbeddingSoItIsReindexed(t *testing.T) {
	db := openIntegrationDB(t)
	ctx := context.Background()

	var user models.User
	if err := db.Where("role = ?", models.AdminRole).First(&user).Error; err != nil {
		t.Skipf("sem usuário admin: %v", err)
	}
	cat, err := AddCategory(ctx, user.ID, "zz-teste-categoria")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM categories WHERE id = ?", cat.ID) })
	col, err := AddCollection(ctx, user.ID, false, "zz-teste-colecao", cat.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM collections WHERE id = ?", col.ID) })
	item, err := AddItem(ctx, user.ID, false, "Moeda de prata", nil, nil, 0, col.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM items WHERE id = ?", item.ID) })

	if err := (IndexStore{}).SetItemEmbedding(ctx, item.ID, models.Vector{0.1, 0.2}); err != nil {
		t.Fatal(err)
	}

	if _, err := UpdateItem(ctx, user.ID, false, item.ID, "Luvas de boxe", nil, nil, 0, col.ID, nil); err != nil {
		t.Fatal(err)
	}

	var stillSet bool
	db.Raw("SELECT embedding IS NOT NULL FROM items WHERE id = ?", item.ID).Scan(&stillSet)
	if stillSet {
		t.Error("embedding kept after the text changed; want NULL so IndexMissing/reindex rebuilds it")
	}
}
