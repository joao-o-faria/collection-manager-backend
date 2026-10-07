package main

import (
	"log"
	"os"
	"time"

	"collection-manager-backend/internal/ai"
	"collection-manager-backend/internal/auth"
	"collection-manager-backend/internal/database"
	"collection-manager-backend/internal/handlers"
	"collection-manager-backend/internal/models"
	"collection-manager-backend/internal/routes"
	"collection-manager-backend/internal/storage"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

func main() {
	if err := godotenv.Load(".env"); err != nil {
		log.Println("aviso: arquivo .env não carregado, usando variáveis de ambiente do sistema")
	}

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		log.Fatal("JWT_SECRET não definida")
	}

	auth.InitJWTSecret(secret)

	db, err := database.NewDB()
	if err != nil {
		log.Fatalf("erro ao conectar no banco: %v", err)
	}

	if err := storage.InitCategoryStorage(db); err != nil {
		log.Fatalf("erro ao inicializar storage de categorias: %v", err)
	}

	if err := storage.InitBinaryObjectStorage(db); err != nil {
		log.Fatalf("erro ao inicializar storage de arquivos: %v", err)
	}

	if err := storage.InitCollectionStorage(db); err != nil {
		log.Fatalf("erro ao inicializar storage de coleções: %v", err)
	}

	if err := storage.InitItemStorage(db); err != nil {
		log.Fatalf("erro ao inicializar storage de itens: %v", err)
	}

	if err := storage.InitUserStorage(db); err != nil {
		log.Fatalf("erro ao inicializar storage de usuários: %v", err)
	}

	createInitialAdmin(db)

	aiClient := ai.NewClient(
		envOrDefault("OLLAMA_URL", "http://localhost:11434"),
		envOrDefault("OLLAMA_MODEL", "gemma4:12b"),
	)
	handlers.InitSuggester(aiClient)
	handlers.InitAnalyzer(aiClient)

	router := gin.Default()

	router.Use(cors.New(cors.Config{
		AllowOrigins: []string{
			"http://localhost:4200",
			"https://collection-manager-frontend.onrender.com",
		},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	routes.RegisterCategoryRoutes(router)
	routes.RegisterCollectionRoutes(router)
	routes.RegisterItemRoutes(router)
	routes.RegisterQuickAddRoutes(router)
	routes.RegisterAuthRoutes(router)

	if err := router.Run(":8080"); err != nil {
		log.Fatalf("erro ao iniciar servidor: %v", err)
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func createInitialAdmin(db *gorm.DB) {
	var count int64
	db.Model(&models.User{}).Where("role = ?", models.AdminRole).Count(&count)
	if count == 0 {
		log.Println("Criando usuário admin inicial...")
		_, err := storage.CreateUser(nil, "Admin", "admin@example.com", "admin123", models.AdminRole)
		if err != nil {
			log.Printf("Erro ao criar admin inicial: %v", err)
		} else {
			log.Println("Usuário admin inicial criado com sucesso: admin@example.com / admin123")
		}
	}
}
