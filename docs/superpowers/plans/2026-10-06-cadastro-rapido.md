# Cadastro rápido por imagem — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Botão "✨ Cadastro rápido" no header que, a partir de uma foto, usa a IA local para sugerir item + categoria/coleção (existentes ou novas), mostra uma tela de revisão e cadastra tudo atomicamente.

**Architecture:** Backend Go ganha `ai.(*Client).AnalyzeItemImage` (Ollama com foto + lista de categorias/coleções do usuário), uma função pura `resolveAnalysis` que valida a escolha do modelo, `POST /quick-add/analyze` e `POST /quick-add` (este último chama `storage.QuickAdd`, que cria categoria/coleção/item numa transação GORM). Frontend Angular ganha `QuickAddService`, o componente `QuickAddModal` (upload → analisando → revisão) e o botão no header do `App`.

**Tech Stack:** Go 1.25, Gin, GORM (Postgres), Ollama `/api/chat`; Angular 21 (standalone, signals), Vitest via `ng test`.

**Spec:** `collection-manager-backend/docs/superpowers/specs/2026-10-06-cadastro-rapido-design.md`

## Global Constraints

- Backend: branch `feature/quick-add` (já criado a partir de `feature/ai-suggest`) em `C:\Users\joaop\Dev\a3\collection-manager-backend`.
- Frontend: criar branch `feature/quick-add` a partir de `feature/ai-suggest` em `C:\Users\joaop\Dev\a3\collection-manager-frontend`.
- Modelo: o mesmo `OLLAMA_MODEL` já configurado (padrão `gemma4:12b`); nenhuma variável de ambiente nova.
- Chamadas ao Ollama: `stream:false`, `think:false`, JSON schema em `format`, foto em `images`.
- Textos de UI e mensagens de erro em português do Brasil.
- IA indisponível → HTTP **503** com `{"error":"Serviço de IA indisponível"}`.
- Nome padrão de categoria nova sem nome: `"Geral"`; coleção nova sem nome: nome da categoria; item sem nome: `"Item sem nome"`.
- A IA não sugere preço; o preço começa em 0.
- Escopo: `scopeByUser` (admin vê tudo; usuário comum só o próprio).
- Commits terminam com `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Testes backend: `go test ./internal/...`. Testes frontend: `npx ng test --watch=false` (os 2 testes de `app.spec.ts` já falhavam antes — NullInjector do RouterLink — e continuam fora do escopo).

## Review Focus

1. **Usuário já está na página de uma coleção ao cadastrar** — `ItemList` lê `collectionId` via `snapshot`, então `router.navigate` para outra (ou a mesma) coleção não recarrega a lista; espera-se ver o item novo. Coberto na Task 6 (navegação força recriação da rota) com teste.
2. **Modelo devolve id de categoria/coleção que não existe ou é de outro usuário** — espera-se tratar como "nova", nunca vazar/usar dado alheio. Coberto na Task 2.
3. **Modelo escolhe coleção existente de outra categoria** — espera-se que a coleção existente mande (categoria ajustada), sem criar inconsistência. Coberto na Task 2; e no `/quick-add`, uma combinação inconsistente enviada pelo cliente vira 400 (Task 4).
4. **Usuário sem nenhuma categoria/coleção (conta nova)** — prompt deve dizer "(nenhuma)" e o fluxo deve sugerir tudo novo. Coberto na Task 1 (prompt) e Task 6 (revisão só com opção "nova").
5. **Usuário troca a categoria na revisão para uma onde a coleção sugerida não existe** — espera-se que a coleção passe para "nova" em vez de enviar par inconsistente. Coberto na Task 6.

---

## File Structure

**Backend (`collection-manager-backend`)**
- Modify `internal/ai/ollama.go` — extrair `chatJSON` reutilizável; `SuggestItemDetails` passa a usá-lo.
- Create `internal/ai/analyze.go` — tipos `CategoryOption`, `CollectionOption`, `RefSuggestion`, `ImageAnalysis` e `(*Client).AnalyzeItemImage`.
- Create `internal/ai/analyze_test.go`.
- Create `internal/handlers/quick_add_resolve.go` — `ResolvedRef`, `QuickAddAnalysisResponse`, `resolveAnalysis`.
- Create `internal/handlers/quick_add_resolve_test.go`.
- Create `internal/handlers/quick_add_handler.go` — `AnalyzeQuickAdd`, `CreateQuickAdd`, injeção (`InitAnalyzer`, vars de pacote).
- Create `internal/handlers/quick_add_handler_test.go`.
- Create `internal/storage/quick_add_storage.go` — `RefInput`, `QuickAddInput`, `ErrCategoryMismatch`, `QuickAdd`.
- Create `internal/routes/quick_add_routes.go`.
- Modify `cmd/api/main.go` — um `ai.Client` compartilhado; `InitAnalyzer`; registrar rotas.

**Frontend (`collection-manager-frontend`)**
- Create `src/app/models/quick-add.model.ts`.
- Create `src/app/services/quick-add.service.ts`.
- Create `src/app/features/quick-add/quick-add-modal.ts`, `.html`, `.scss`, `.spec.ts`.
- Modify `src/app/app.ts`, `src/app/app.html`, `src/app/app.scss` — botão no header e montagem do modal.

---

### Task 1: `ai.AnalyzeItemImage`

**Files:**
- Modify: `internal/ai/ollama.go`
- Create: `internal/ai/analyze.go`
- Test: `internal/ai/analyze_test.go`

**Interfaces:**
- Consumes: `Client`, `chatMessage`, `chatRequest`, `chatResponse`, `ErrUnavailable` (já existem em `ollama.go`).
- Produces:
  ```go
  type CategoryOption struct { ID int; Name string }
  type CollectionOption struct { ID int; Name string; CategoryID int }
  type RefSuggestion struct { ID int `json:"id"`; NewName string `json:"new_name"` }
  type ImageAnalysis struct {
      Name string `json:"name"`; Description string `json:"description"`; Tags []string `json:"tags"`
      Category RefSuggestion `json:"category"`; Collection RefSuggestion `json:"collection"`
  }
  func (c *Client) AnalyzeItemImage(ctx context.Context, imageBase64 string, categories []CategoryOption, collections []CollectionOption) (ImageAnalysis, error)
  ```

- [ ] **Step 1: Write the failing tests** — `internal/ai/analyze_test.go` (reutiliza `capturedRequest` e `newFakeOllama` de `ollama_test.go`, mesmo pacote):

```go
package ai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnalyzeItemImage_ParsesModelReply(t *testing.T) {
	var got capturedRequest
	reply := `{"name":"Moeda de 1 real","description":"Moeda prateada.","tags":["moeda"],` +
		`"category":{"id":3,"new_name":""},"collection":{"id":0,"new_name":"Moedas brasileiras"}}`
	srv := newFakeOllama(t, reply, &got)
	defer srv.Close()

	a, err := NewClient(srv.URL, "gemma4:12b").AnalyzeItemImage(context.Background(), "aW1n", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Name != "Moeda de 1 real" || a.Description != "Moeda prateada." || len(a.Tags) != 1 {
		t.Errorf("analysis = %+v", a)
	}
	if a.Category.ID != 3 || a.Collection.ID != 0 || a.Collection.NewName != "Moedas brasileiras" {
		t.Errorf("refs = %+v / %+v", a.Category, a.Collection)
	}
}

func TestAnalyzeItemImage_SendsImageSchemaAndOptions(t *testing.T) {
	var got capturedRequest
	srv := newFakeOllama(t, `{"name":"x","description":"","tags":[],"category":{"id":0,"new_name":"a"},"collection":{"id":0,"new_name":"b"}}`, &got)
	defer srv.Close()

	cats := []CategoryOption{{ID: 3, Name: "Numismática"}}
	cols := []CollectionOption{{ID: 7, Name: "Moedas antigas", CategoryID: 3}}
	_, err := NewClient(srv.URL, "gemma4:12b").AnalyzeItemImage(context.Background(), "aW1n", cats, cols)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Model != "gemma4:12b" || got.Stream || got.Think || got.Format == nil {
		t.Errorf("model=%q stream=%v think=%v format=%v", got.Model, got.Stream, got.Think, got.Format)
	}
	user := got.Messages[len(got.Messages)-1]
	if len(user.Images) != 1 || user.Images[0] != "aW1n" {
		t.Errorf("images = %v", user.Images)
	}
	for _, want := range []string{"3: Numismática", "7: Moedas antigas (categoria 3)"} {
		if !strings.Contains(user.Content, want) {
			t.Errorf("prompt missing %q:\n%s", want, user.Content)
		}
	}
}

func TestAnalyzeItemImage_SaysNoneWhenUserHasNoOptions(t *testing.T) {
	var got capturedRequest
	srv := newFakeOllama(t, `{"name":"x","description":"","tags":[],"category":{"id":0,"new_name":"a"},"collection":{"id":0,"new_name":"b"}}`, &got)
	defer srv.Close()

	_, err := NewClient(srv.URL, "gemma4:12b").AnalyzeItemImage(context.Background(), "aW1n", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	user := got.Messages[len(got.Messages)-1]
	if strings.Count(user.Content, "(nenhuma)") != 2 {
		t.Errorf("want '(nenhuma)' for categories and collections:\n%s", user.Content)
	}
}

func TestAnalyzeItemImage_ErrUnavailableOnServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "gemma4:12b").AnalyzeItemImage(context.Background(), "aW1n", nil, nil)
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ai/`
Expected: build failure — `undefined: CategoryOption`, `AnalyzeItemImage`.

- [ ] **Step 3: Extract `chatJSON` in `ollama.go`** — substituir o corpo de `SuggestItemDetails` (do `body, err := json.Marshal(...)` até o fim) por uma chamada ao helper, e adicionar o helper:

```go
func (c *Client) SuggestItemDetails(ctx context.Context, item ItemContext) (ItemSuggestion, error) {
	prompt := fmt.Sprintf("Item: %s\nColeção: %s\nCategoria: %s", item.Name, item.Collection, item.Category)
	var suggestion ItemSuggestion
	err := c.chatJSON(ctx, systemPrompt, prompt, item.ImageBase64, suggestionSchema, &suggestion)
	return suggestion, err
}

// chatJSON envia uma conversa (system + user, com foto opcional) ao Ollama
// exigindo resposta no JSON schema informado e decodifica o resultado em out.
func (c *Client) chatJSON(ctx context.Context, system, prompt, imageBase64 string, schema any, out any) error {
	user := chatMessage{Role: "user", Content: prompt}
	if imageBase64 != "" {
		user.Images = []string{imageBase64}
	}

	body, err := json.Marshal(chatRequest{
		Model:    c.model,
		Messages: []chatMessage{{Role: "system", Content: system}, user},
		Format:   schema,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}

	var chat chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chat); err != nil {
		return fmt.Errorf("%w: resposta inválida: %v", ErrUnavailable, err)
	}
	if err := json.Unmarshal([]byte(chat.Message.Content), out); err != nil {
		return fmt.Errorf("%w: JSON do modelo inválido: %v", ErrUnavailable, err)
	}
	return nil
}
```

- [ ] **Step 4: Create `internal/ai/analyze.go`**

```go
package ai

import (
	"context"
	"fmt"
	"strings"
)

type CategoryOption struct {
	ID   int
	Name string
}

type CollectionOption struct {
	ID         int
	Name       string
	CategoryID int
}

// RefSuggestion aponta para uma entidade existente (ID > 0) ou sugere criar uma nova (ID == 0, NewName).
type RefSuggestion struct {
	ID      int    `json:"id"`
	NewName string `json:"new_name"`
}

type ImageAnalysis struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Tags        []string      `json:"tags"`
	Category    RefSuggestion `json:"category"`
	Collection  RefSuggestion `json:"collection"`
}

const analyzeSystemPrompt = `Você é um assistente de um sistema de gerenciamento de coleções.
Analise a foto de um item e responda em português do Brasil com:
- "name": um nome curto para o item.
- "description": uma descrição curta e objetiva (2 a 3 frases) do que é visível (cor, material, estado de conservação). Não invente preço.
- "tags": de 3 a 6 tags curtas, em letras minúsculas.
- "category": a categoria do item. Se uma das categorias existentes servir, use {"id": <id dela>, "new_name": ""}. Se nenhuma servir, use {"id": 0, "new_name": "<nome da nova categoria>"}.
- "collection": a coleção do item. Se uma das coleções existentes servir E pertencer à categoria escolhida, use {"id": <id dela>, "new_name": ""}. Caso contrário, use {"id": 0, "new_name": "<nome da nova coleção>"}.
Prefira sempre reaproveitar categorias e coleções existentes quando fizer sentido, mesmo que os nomes não sejam idênticos.
Responda somente com o JSON pedido.`

var refSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"id":       map[string]any{"type": "integer"},
		"new_name": map[string]any{"type": "string"},
	},
	"required": []string{"id", "new_name"},
}

var analysisSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"name":        map[string]any{"type": "string"},
		"description": map[string]any{"type": "string"},
		"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"category":    refSchema,
		"collection":  refSchema,
	},
	"required": []string{"name", "description", "tags", "category", "collection"},
}

func (c *Client) AnalyzeItemImage(ctx context.Context, imageBase64 string, categories []CategoryOption, collections []CollectionOption) (ImageAnalysis, error) {
	var b strings.Builder
	b.WriteString("Categorias existentes:\n")
	if len(categories) == 0 {
		b.WriteString("(nenhuma)\n")
	}
	for _, cat := range categories {
		fmt.Fprintf(&b, "- %d: %s\n", cat.ID, cat.Name)
	}
	b.WriteString("\nColeções existentes:\n")
	if len(collections) == 0 {
		b.WriteString("(nenhuma)\n")
	}
	for _, col := range collections {
		fmt.Fprintf(&b, "- %d: %s (categoria %d)\n", col.ID, col.Name, col.CategoryID)
	}

	var analysis ImageAnalysis
	err := c.chatJSON(ctx, analyzeSystemPrompt, b.String(), imageBase64, analysisSchema, &analysis)
	return analysis, err
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/ai/`
Expected: PASS (os 5 testes antigos de `SuggestItemDetails` + 4 novos).

- [ ] **Step 6: Commit**

```bash
git add internal/ai
git commit -m "feat(ai): analyze item photo and pick or propose category/collection

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: `resolveAnalysis` (validação pura)

**Files:**
- Create: `internal/handlers/quick_add_resolve.go`
- Test: `internal/handlers/quick_add_resolve_test.go`

**Interfaces:**
- Consumes: `ai.ImageAnalysis`, `ai.RefSuggestion` (Task 1); `models.Category`, `models.Collection`; `normalizeTags` (já existe em `item_handler.go`).
- Produces:
  ```go
  type ResolvedRef struct { ID int `json:"id"`; Name string `json:"name"`; IsNew bool `json:"is_new"` }
  type QuickAddAnalysisResponse struct {
      Name string `json:"name"`; Description string `json:"description"`; Tags []string `json:"tags"`
      Category ResolvedRef `json:"category"`; Collection ResolvedRef `json:"collection"`
  }
  func resolveAnalysis(a ai.ImageAnalysis, categories []models.Category, collections []models.Collection) QuickAddAnalysisResponse
  ```

- [ ] **Step 1: Write the failing tests**

```go
package handlers

import (
	"fmt"
	"testing"

	"collection-manager-backend/internal/ai"
	"collection-manager-backend/internal/models"
)

var (
	testCats = []models.Category{{ID: 1, Name: "Numismática"}, {ID: 2, Name: "Miniaturas"}}
	testCols = []models.Collection{
		{ID: 10, Name: "Moedas antigas", CategoryID: 1},
		{ID: 20, Name: "Hot Wheels", CategoryID: 2},
	}
)

func analysis(cat, col ai.RefSuggestion) ai.ImageAnalysis {
	return ai.ImageAnalysis{Name: "Moeda", Description: "desc", Tags: []string{"moeda"}, Category: cat, Collection: col}
}

func TestResolve_KeepsValidExistingRefs(t *testing.T) {
	r := resolveAnalysis(analysis(ai.RefSuggestion{ID: 1}, ai.RefSuggestion{ID: 10}), testCats, testCols)
	if r.Category != (ResolvedRef{ID: 1, Name: "Numismática"}) || r.Collection != (ResolvedRef{ID: 10, Name: "Moedas antigas"}) {
		t.Errorf("got %+v / %+v", r.Category, r.Collection)
	}
}

func TestResolve_KeepsNewRefs(t *testing.T) {
	r := resolveAnalysis(analysis(ai.RefSuggestion{NewName: " Selos "}, ai.RefSuggestion{NewName: "Selos raros"}), testCats, testCols)
	if r.Category != (ResolvedRef{Name: "Selos", IsNew: true}) || r.Collection != (ResolvedRef{Name: "Selos raros", IsNew: true}) {
		t.Errorf("got %+v / %+v", r.Category, r.Collection)
	}
}

func TestResolve_UnknownCategoryIDBecomesNew(t *testing.T) {
	r := resolveAnalysis(analysis(ai.RefSuggestion{ID: 99, NewName: "Selos"}, ai.RefSuggestion{NewName: "Selos raros"}), testCats, testCols)
	if r.Category != (ResolvedRef{Name: "Selos", IsNew: true}) {
		t.Errorf("category = %+v", r.Category)
	}
}

func TestResolve_UnknownCategoryWithoutNameBecomesGeral(t *testing.T) {
	r := resolveAnalysis(analysis(ai.RefSuggestion{ID: 99}, ai.RefSuggestion{}), testCats, testCols)
	if r.Category != (ResolvedRef{Name: "Geral", IsNew: true}) || r.Collection != (ResolvedRef{Name: "Geral", IsNew: true}) {
		t.Errorf("got %+v / %+v", r.Category, r.Collection)
	}
}

func TestResolve_UnknownCollectionIDBecomesNewNamedAfterCategory(t *testing.T) {
	r := resolveAnalysis(analysis(ai.RefSuggestion{ID: 1}, ai.RefSuggestion{ID: 99}), testCats, testCols)
	if r.Collection != (ResolvedRef{Name: "Numismática", IsNew: true}) {
		t.Errorf("collection = %+v", r.Collection)
	}
}

func TestResolve_ExistingCollectionOverridesMismatchedCategory(t *testing.T) {
	r := resolveAnalysis(analysis(ai.RefSuggestion{ID: 1}, ai.RefSuggestion{ID: 20}), testCats, testCols)
	if r.Category != (ResolvedRef{ID: 2, Name: "Miniaturas"}) || r.Collection != (ResolvedRef{ID: 20, Name: "Hot Wheels"}) {
		t.Errorf("got %+v / %+v", r.Category, r.Collection)
	}
}

func TestResolve_NewCategoryForcesNewCollection(t *testing.T) {
	r := resolveAnalysis(analysis(ai.RefSuggestion{NewName: "Selos"}, ai.RefSuggestion{ID: 10}), testCats, testCols)
	if r.Category != (ResolvedRef{Name: "Selos", IsNew: true}) || r.Collection != (ResolvedRef{Name: "Selos", IsNew: true}) {
		t.Errorf("got %+v / %+v", r.Category, r.Collection)
	}
}

func TestResolve_NormalizesItemFields(t *testing.T) {
	a := ai.ImageAnalysis{Name: "  ", Description: " d ", Tags: []string{" a ", "", "a"},
		Category: ai.RefSuggestion{ID: 1}, Collection: ai.RefSuggestion{ID: 10}}
	r := resolveAnalysis(a, testCats, testCols)
	if r.Name != "Item sem nome" || r.Description != "d" || fmt.Sprint(r.Tags) != "[a]" {
		t.Errorf("got %+v", r)
	}
}

func TestResolve_NilTagsBecomeEmptySlice(t *testing.T) {
	a := ai.ImageAnalysis{Name: "x", Category: ai.RefSuggestion{ID: 1}, Collection: ai.RefSuggestion{ID: 10}}
	if r := resolveAnalysis(a, testCats, testCols); r.Tags == nil {
		t.Error("tags = nil, want empty slice")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/handlers/ -run TestResolve`
Expected: build failure — `undefined: resolveAnalysis`, `ResolvedRef`.

- [ ] **Step 3: Implement `internal/handlers/quick_add_resolve.go`**

```go
package handlers

import (
	"strings"

	"collection-manager-backend/internal/ai"
	"collection-manager-backend/internal/models"
)

type ResolvedRef struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	IsNew bool   `json:"is_new"`
}

type QuickAddAnalysisResponse struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Tags        []string    `json:"tags"`
	Category    ResolvedRef `json:"category"`
	Collection  ResolvedRef `json:"collection"`
}

// resolveAnalysis valida a escolha do modelo contra as categorias/coleções
// realmente visíveis ao usuário, garantindo um par categoria/coleção coerente.
func resolveAnalysis(a ai.ImageAnalysis, categories []models.Category, collections []models.Collection) QuickAddAnalysisResponse {
	catByID := make(map[int]models.Category, len(categories))
	for _, c := range categories {
		catByID[c.ID] = c
	}
	colByID := make(map[int]models.Collection, len(collections))
	for _, c := range collections {
		colByID[c.ID] = c
	}

	var category ResolvedRef
	if c, ok := catByID[a.Category.ID]; ok && a.Category.ID > 0 {
		category = ResolvedRef{ID: c.ID, Name: c.Name}
	} else {
		category = ResolvedRef{Name: orDefault(a.Category.NewName, "Geral"), IsNew: true}
	}

	var collection ResolvedRef
	col, colExists := colByID[a.Collection.ID]
	switch {
	case colExists && a.Collection.ID > 0 && !category.IsNew:
		if col.CategoryID != category.ID {
			if owner, ok := catByID[col.CategoryID]; ok {
				category = ResolvedRef{ID: owner.ID, Name: owner.Name}
			}
		}
		if col.CategoryID == category.ID {
			collection = ResolvedRef{ID: col.ID, Name: col.Name}
			break
		}
		fallthrough
	default:
		newName := a.Collection.NewName
		if colExists && a.Collection.ID > 0 {
			newName = ""
		}
		collection = ResolvedRef{Name: orDefault(newName, category.Name), IsNew: true}
	}

	tags := normalizeTags(a.Tags)
	if tags == nil {
		tags = []string{}
	}

	return QuickAddAnalysisResponse{
		Name:        orDefault(a.Name, "Item sem nome"),
		Description: strings.TrimSpace(a.Description),
		Tags:        tags,
		Category:    category,
		Collection:  collection,
	}
}

func orDefault(s, fallback string) string {
	if trimmed := strings.TrimSpace(s); trimmed != "" {
		return trimmed
	}
	return fallback
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/handlers/`
Expected: PASS (testes novos + os 5 de `suggest_handler_test.go`).

- [ ] **Step 5: Commit**

```bash
git add internal/handlers/quick_add_resolve.go internal/handlers/quick_add_resolve_test.go
git commit -m "feat(api): validate AI category/collection choice against user's data

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: `POST /quick-add/analyze`

**Files:**
- Create: `internal/handlers/quick_add_handler.go`
- Create: `internal/routes/quick_add_routes.go`
- Modify: `cmd/api/main.go`
- Test: `internal/handlers/quick_add_handler_test.go`

**Interfaces:**
- Consumes: `ai.(*Client).AnalyzeItemImage`, `ai.CategoryOption`, `ai.CollectionOption`, `ai.ErrUnavailable` (Task 1); `resolveAnalysis`, `QuickAddAnalysisResponse` (Task 2); `storage.GetCategories(ctx, userID, isAdmin, search) ([]models.Category, error)`, `storage.GetCollections(ctx, userID, isAdmin, search) ([]models.Collection, error)`; `actorFromContext`.
- Produces: `handlers.AnalyzeQuickAdd(c *gin.Context)`, `handlers.InitAnalyzer(a imageAnalyzer)`, `routes.RegisterQuickAddRoutes(r *gin.Engine)` (registra só `/analyze` aqui; Task 4 adiciona `POST ""`). Variáveis de pacote `analyzer`, `listCategories`, `listCollections` para teste.

- [ ] **Step 1: Write the failing tests** — `internal/handlers/quick_add_handler_test.go`:

```go
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"collection-manager-backend/internal/ai"
	"collection-manager-backend/internal/models"

	"github.com/gin-gonic/gin"
)

type fakeAnalyzer struct {
	gotImage string
	gotCats  []ai.CategoryOption
	gotCols  []ai.CollectionOption
	result   ai.ImageAnalysis
	err      error
}

func (f *fakeAnalyzer) AnalyzeItemImage(_ context.Context, img string, cats []ai.CategoryOption, cols []ai.CollectionOption) (ai.ImageAnalysis, error) {
	f.gotImage, f.gotCats, f.gotCols = img, cats, cols
	return f.result, f.err
}

func newQuickAddRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	withActor := func(h gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			c.Set("user_id", uint(7))
			c.Set("user_role", models.UserRole)
			h(c)
		}
	}
	r.POST("/quick-add/analyze", withActor(AnalyzeQuickAdd))
	r.POST("/quick-add", withActor(CreateQuickAdd))
	return r
}

func setupAnalyze(t *testing.T, a imageAnalyzer) *gin.Engine {
	t.Helper()
	prevA, prevCats, prevCols := analyzer, listCategories, listCollections
	analyzer = a
	listCategories = func(_ context.Context, userID uint, isAdmin bool, _ string) ([]models.Category, error) {
		return testCats, nil
	}
	listCollections = func(_ context.Context, userID uint, isAdmin bool, _ string) ([]models.Collection, error) {
		return testCols, nil
	}
	t.Cleanup(func() { analyzer, listCategories, listCollections = prevA, prevCats, prevCols })
	return newQuickAddRouter(t)
}

func post(r *gin.Engine, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestAnalyzeQuickAdd_ReturnsResolvedAnalysis(t *testing.T) {
	fake := &fakeAnalyzer{result: ai.ImageAnalysis{
		Name: "Moeda", Description: "d", Tags: []string{"moeda"},
		Category: ai.RefSuggestion{ID: 1}, Collection: ai.RefSuggestion{ID: 99, NewName: "Moedas de prata"},
	}}
	r := setupAnalyze(t, fake)

	w := post(r, "/quick-add/analyze", `{"image_base64":"aW1n"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	var resp QuickAddAnalysisResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Category != (ResolvedRef{ID: 1, Name: "Numismática"}) || resp.Collection != (ResolvedRef{Name: "Moedas de prata", IsNew: true}) {
		t.Errorf("resp = %+v", resp)
	}
	if fake.gotImage != "aW1n" {
		t.Errorf("image = %q", fake.gotImage)
	}
	if len(fake.gotCats) != 2 || fake.gotCats[0] != (ai.CategoryOption{ID: 1, Name: "Numismática"}) {
		t.Errorf("cats = %+v", fake.gotCats)
	}
	if len(fake.gotCols) != 2 || fake.gotCols[1] != (ai.CollectionOption{ID: 20, Name: "Hot Wheels", CategoryID: 2}) {
		t.Errorf("cols = %+v", fake.gotCols)
	}
}

func TestAnalyzeQuickAdd_RejectsMissingImage(t *testing.T) {
	r := setupAnalyze(t, &fakeAnalyzer{})

	if w := post(r, "/quick-add/analyze", `{"image_base64":"  "}`); w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAnalyzeQuickAdd_ServiceUnavailableWhenAIDown(t *testing.T) {
	r := setupAnalyze(t, &fakeAnalyzer{err: ai.ErrUnavailable})

	if w := post(r, "/quick-add/analyze", `{"image_base64":"aW1n"}`); w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/handlers/ -run TestAnalyzeQuickAdd`
Expected: build failure — `undefined: AnalyzeQuickAdd`, `CreateQuickAdd`, `imageAnalyzer`, `analyzer`, `listCategories`, `listCollections`.

- [ ] **Step 3: Implement `internal/handlers/quick_add_handler.go`** (a parte de criação vem na Task 4; aqui inclua um `CreateQuickAdd` provisório que responde 501 só para compilar, substituído na Task 4):

```go
package handlers

import (
	"context"
	"net/http"
	"strings"

	"collection-manager-backend/internal/ai"
	"collection-manager-backend/internal/storage"

	"github.com/gin-gonic/gin"
)

type imageAnalyzer interface {
	AnalyzeItemImage(ctx context.Context, imageBase64 string, categories []ai.CategoryOption, collections []ai.CollectionOption) (ai.ImageAnalysis, error)
}

var (
	analyzer        imageAnalyzer
	listCategories  = storage.GetCategories
	listCollections = storage.GetCollections
)

// InitAnalyzer define o cliente de IA usado pelo cadastro rápido.
func InitAnalyzer(a imageAnalyzer) {
	analyzer = a
}

type AnalyzeQuickAddInput struct {
	ImageBase64 string `json:"image_base64"`
}

func AnalyzeQuickAdd(c *gin.Context) {
	userID, isAdmin, ok := actorFromContext(c)
	if !ok {
		return
	}

	var input AnalyzeQuickAddInput
	if err := c.ShouldBindJSON(&input); err != nil || strings.TrimSpace(input.ImageBase64) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Imagem obrigatória"})
		return
	}

	ctx := c.Request.Context()
	categories, err := listCategories(ctx, userID, isAdmin, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar categorias"})
		return
	}
	collections, err := listCollections(ctx, userID, isAdmin, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar coleções"})
		return
	}

	if analyzer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Serviço de IA indisponível"})
		return
	}

	catOptions := make([]ai.CategoryOption, 0, len(categories))
	for _, cat := range categories {
		catOptions = append(catOptions, ai.CategoryOption{ID: cat.ID, Name: cat.Name})
	}
	colOptions := make([]ai.CollectionOption, 0, len(collections))
	for _, col := range collections {
		colOptions = append(colOptions, ai.CollectionOption{ID: col.ID, Name: col.Name, CategoryID: col.CategoryID})
	}

	analysis, err := analyzer.AnalyzeItemImage(ctx, input.ImageBase64, catOptions, colOptions)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Serviço de IA indisponível"})
		return
	}

	c.JSON(http.StatusOK, resolveAnalysis(analysis, categories, collections))
}

func CreateQuickAdd(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "não implementado"})
}
```

- [ ] **Step 4: Create `internal/routes/quick_add_routes.go`**

```go
package routes

import (
	"collection-manager-backend/internal/handlers"
	"collection-manager-backend/internal/middleware"

	"github.com/gin-gonic/gin"
)

func RegisterQuickAddRoutes(r *gin.Engine) {
	quickAdd := r.Group("/quick-add")
	quickAdd.Use(middleware.AuthMiddleware())
	{
		quickAdd.POST("/analyze", handlers.AnalyzeQuickAdd)
	}
}
```

- [ ] **Step 5: Wire in `cmd/api/main.go`** — substituir o bloco `handlers.InitSuggester(ai.NewClient(...))` por um cliente compartilhado, e registrar as rotas após `routes.RegisterItemRoutes(router)`:

```go
	aiClient := ai.NewClient(
		envOrDefault("OLLAMA_URL", "http://localhost:11434"),
		envOrDefault("OLLAMA_MODEL", "gemma4:12b"),
	)
	handlers.InitSuggester(aiClient)
	handlers.InitAnalyzer(aiClient)
```

```go
	routes.RegisterItemRoutes(router)
	routes.RegisterQuickAddRoutes(router)
```

- [ ] **Step 6: Run tests and build**

Run: `go test ./internal/... && go vet ./... && go build ./...`
Expected: PASS, sem erros.

- [ ] **Step 7: Commit**

```bash
git add internal/handlers/quick_add_handler.go internal/handlers/quick_add_handler_test.go internal/routes/quick_add_routes.go cmd/api/main.go
git commit -m "feat(api): add POST /quick-add/analyze

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: `storage.QuickAdd` + `POST /quick-add`

**Files:**
- Create: `internal/storage/quick_add_storage.go`
- Modify: `internal/handlers/quick_add_handler.go` (substituir o `CreateQuickAdd` provisório)
- Modify: `internal/routes/quick_add_routes.go`
- Test: `internal/handlers/quick_add_handler_test.go` (adicionar testes)

**Interfaces:**
- Consumes: `itemDB` (`*gorm.DB` do pacote storage), `scopeByUser`, `ErrNotFound`, `BinaryObjectPayload`, `models.*`; `newQuickAddRouter`, `post` (Task 3); `normalizeDescription`, `normalizeTags`, `toPayload`, `BinaryObjectInput` (já existem nos handlers).
- Produces:
  ```go
  // storage
  type RefInput struct { ID int; NewName string }
  type QuickAddInput struct {
      Category RefInput; Collection RefInput
      Name string; Description *string; Tags []string; Price float64; Binary *BinaryObjectPayload
  }
  var ErrCategoryMismatch = errors.New("coleção não pertence à categoria informada")
  func QuickAdd(ctx context.Context, userID uint, isAdmin bool, in QuickAddInput) (models.Item, error)
  // handlers
  var quickAdd = storage.QuickAdd
  func CreateQuickAdd(c *gin.Context)
  ```

- [ ] **Step 1: Write the failing handler tests** — acrescentar a `quick_add_handler_test.go` (adicionar `"errors"` e `"collection-manager-backend/internal/storage"` aos imports):

```go
func setupCreate(t *testing.T, fn func(context.Context, uint, bool, storage.QuickAddInput) (models.Item, error)) *gin.Engine {
	t.Helper()
	prev := quickAdd
	quickAdd = fn
	t.Cleanup(func() { quickAdd = prev })
	return newQuickAddRouter(t)
}

const validCreateBody = `{
  "category": {"new_name": " Selos "},
  "collection": {"new_name": "Selos raros"},
  "item": {"name": " Selo azul ", "description": " d ", "tags": ["selo", "selo"], "price": 12.5,
           "binary_object": {"base64": "aW1n", "filename": "a.jpg", "extension": "jpg"}}
}`

func TestCreateQuickAdd_PassesNormalizedInputAndReturns201(t *testing.T) {
	var got storage.QuickAddInput
	r := setupCreate(t, func(_ context.Context, userID uint, isAdmin bool, in storage.QuickAddInput) (models.Item, error) {
		got = in
		return models.Item{ID: 5, Name: in.Name, CollectionID: 42}, nil
	})

	w := post(r, "/quick-add", validCreateBody)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	if got.Category != (storage.RefInput{NewName: "Selos"}) || got.Collection != (storage.RefInput{NewName: "Selos raros"}) {
		t.Errorf("refs = %+v / %+v", got.Category, got.Collection)
	}
	if got.Name != "Selo azul" || got.Description == nil || *got.Description != "d" || len(got.Tags) != 1 || got.Price != 12.5 {
		t.Errorf("item = %+v", got)
	}
	if got.Binary == nil || got.Binary.Base64 != "aW1n" {
		t.Errorf("binary = %+v", got.Binary)
	}
	var item models.Item
	_ = json.Unmarshal(w.Body.Bytes(), &item)
	if item.CollectionID != 42 {
		t.Errorf("collection_id = %d", item.CollectionID)
	}
}

func TestCreateQuickAdd_AcceptsExistingIDs(t *testing.T) {
	var got storage.QuickAddInput
	r := setupCreate(t, func(_ context.Context, _ uint, _ bool, in storage.QuickAddInput) (models.Item, error) {
		got = in
		return models.Item{ID: 1}, nil
	})

	w := post(r, "/quick-add", `{"category":{"id":1},"collection":{"id":10},"item":{"name":"x","price":0}}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	if got.Category != (storage.RefInput{ID: 1}) || got.Collection != (storage.RefInput{ID: 10}) {
		t.Errorf("refs = %+v / %+v", got.Category, got.Collection)
	}
}

func TestCreateQuickAdd_ValidationErrors(t *testing.T) {
	called := false
	r := setupCreate(t, func(context.Context, uint, bool, storage.QuickAddInput) (models.Item, error) {
		called = true
		return models.Item{}, nil
	})

	cases := map[string]string{
		"ref with id and name":             `{"category":{"id":1,"new_name":"a"},"collection":{"id":10},"item":{"name":"x"}}`,
		"ref with neither":                 `{"category":{},"collection":{"id":10},"item":{"name":"x"}}`,
		"blank new name":                   `{"category":{"new_name":"  "},"collection":{"new_name":"b"},"item":{"name":"x"}}`,
		"new category + existing collection": `{"category":{"new_name":"a"},"collection":{"id":10},"item":{"name":"x"}}`,
		"blank item name":                  `{"category":{"id":1},"collection":{"id":10},"item":{"name":"  "}}`,
		"negative price":                   `{"category":{"id":1},"collection":{"id":10},"item":{"name":"x","price":-1}}`,
	}
	for name, body := range cases {
		if w := post(r, "/quick-add", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, w.Code)
		}
	}
	if called {
		t.Error("storage.QuickAdd should not be called on invalid input")
	}
}

func TestCreateQuickAdd_MapsStorageErrors(t *testing.T) {
	cases := map[error]int{
		storage.ErrNotFound:         http.StatusNotFound,
		storage.ErrCategoryMismatch: http.StatusBadRequest,
		errors.New("db down"):       http.StatusInternalServerError,
	}
	for storageErr, want := range cases {
		r := setupCreate(t, func(context.Context, uint, bool, storage.QuickAddInput) (models.Item, error) {
			return models.Item{}, storageErr
		})
		w := post(r, "/quick-add", `{"category":{"id":1},"collection":{"id":10},"item":{"name":"x"}}`)
		if w.Code != want {
			t.Errorf("%v: status = %d, want %d", storageErr, w.Code, want)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/handlers/ -run TestCreateQuickAdd`
Expected: build failure — `undefined: quickAdd`, `storage.QuickAddInput`, `storage.RefInput`, `storage.ErrCategoryMismatch`.

- [ ] **Step 3: Implement `internal/storage/quick_add_storage.go`**

```go
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
```

- [ ] **Step 4: Replace the provisional `CreateQuickAdd`** in `internal/handlers/quick_add_handler.go` (adicionar `"errors"` aos imports e `quickAdd = storage.QuickAdd` ao bloco `var`):

```go
var quickAdd = storage.QuickAdd

type QuickAddRefInput struct {
	ID      int    `json:"id"`
	NewName string `json:"new_name"`
}

type QuickAddItemInput struct {
	Name         string             `json:"name"`
	Description  *string            `json:"description"`
	Tags         []string           `json:"tags"`
	Price        float64            `json:"price"`
	BinaryObject *BinaryObjectInput `json:"binary_object"`
}

type CreateQuickAddInput struct {
	Category   QuickAddRefInput  `json:"category"`
	Collection QuickAddRefInput  `json:"collection"`
	Item       QuickAddItemInput `json:"item"`
}

// toRefInput aceita exatamente um entre id (> 0) e new_name (não vazio).
func toRefInput(r QuickAddRefInput) (storage.RefInput, bool) {
	name := strings.TrimSpace(r.NewName)
	switch {
	case r.ID > 0 && name == "":
		return storage.RefInput{ID: r.ID}, true
	case r.ID == 0 && name != "":
		return storage.RefInput{NewName: name}, true
	default:
		return storage.RefInput{}, false
	}
}

func CreateQuickAdd(c *gin.Context) {
	userID, isAdmin, ok := actorFromContext(c)
	if !ok {
		return
	}

	var input CreateQuickAddInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	category, okCat := toRefInput(input.Category)
	collection, okCol := toRefInput(input.Collection)
	if !okCat || !okCol {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Informe id ou nome de categoria e coleção"})
		return
	}
	if category.ID == 0 && collection.ID != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Categoria nova exige coleção nova"})
		return
	}
	name := strings.TrimSpace(input.Item.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nome não pode ser vazio"})
		return
	}
	if input.Item.Price < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Valor inválido"})
		return
	}

	item, err := quickAdd(c.Request.Context(), userID, isAdmin, storage.QuickAddInput{
		Category:    category,
		Collection:  collection,
		Name:        name,
		Description: normalizeDescription(input.Item.Description),
		Tags:        normalizeTags(input.Item.Tags),
		Price:       input.Item.Price,
		Binary:      toPayload(input.Item.BinaryObject),
	})
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "Categoria ou coleção não encontrada"})
		case errors.Is(err, storage.ErrCategoryMismatch):
			c.JSON(http.StatusBadRequest, gin.H{"error": "A coleção não pertence à categoria informada"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao cadastrar item"})
		}
		return
	}

	c.JSON(http.StatusCreated, item)
}
```

- [ ] **Step 5: Register the route** — em `quick_add_routes.go`, dentro do bloco:

```go
		quickAdd.POST("", handlers.CreateQuickAdd)
		quickAdd.POST("/analyze", handlers.AnalyzeQuickAdd)
```

- [ ] **Step 6: Run tests and build**

Run: `go test ./internal/... && go vet ./... && go build ./...`
Expected: PASS.

- [ ] **Step 7: Verify `QuickAdd` against the real database** (Postgres `collection-manager-db` em `localhost:5432`, admin `admin@example.com`/`admin123`). Reiniciar o backend (`go run ./cmd/api`) e executar:

```bash
B=http://localhost:8080
TOKEN=$(curl -s $B/auth/login -H 'Content-Type: application/json' -d '{"email":"admin@example.com","password":"admin123"}' | python -c "import sys,json;print(json.load(sys.stdin)['token'])")
H="Authorization: Bearer $TOKEN"; J='Content-Type: application/json'
# (a) tudo novo → 201, cria categoria + coleção + item
curl -s -w ' [%{http_code}]\n' $B/quick-add -H "$H" -H "$J" -d '{"category":{"new_name":"Selos"},"collection":{"new_name":"Selos raros"},"item":{"name":"Selo azul","price":3}}'
# (b) existentes → 201 (ids da categoria "Numismática" e coleção "Moedas brasileiras" criadas antes: 1 e 1)
curl -s -w ' [%{http_code}]
' $B/quick-add -H "$H" -H "$J" -d '{"category":{"id":1},"collection":{"id":1},"item":{"name":"Moeda 50 centavos","price":1}}'
# (c) coleção de outra categoria → 400 (coleção 1 pertence à categoria 1; a categoria "Selos" criada em (a) tem outro id)
SELOS=$(curl -s $B/categories -H "$H" | python -c "import sys,json;print([c['id'] for c in json.load(sys.stdin) if c['name']=='Selos'][0])")
curl -s -w ' [%{http_code}]
' $B/quick-add -H "$H" -H "$J" -d "{\"category\":{\"id\":$SELOS},\"collection\":{\"id\":1},\"item\":{\"name\":\"x\"}}"
# (d) id inexistente → 404
curl -s -w ' [%{http_code}]
' $B/quick-add -H "$H" -H "$J" -d '{"category":{"id":9999},"collection":{"new_name":"x"},"item":{"name":"x"}}'
# (e) rollback: categoria e coleção novas + item com price 1e9 (estoura NUMERIC(10,2)) → 500,
#     e nada da transação pode ficar gravado
curl -s -w ' [%{http_code}]\n' $B/quick-add -H "$H" -H "$J" -d '{"category":{"new_name":"Rollback"},"collection":{"new_name":"Rollback"},"item":{"name":"x","price":1000000000}}'
curl -s $B/categories -H "$H"   # "Rollback" NÃO deve aparecer
```

Expected: (a) 201; (b) 201; (c) 400; (d) 404; (e) 500 e nenhuma categoria "Rollback" criada.

- [ ] **Step 8: Commit**

```bash
git add internal/storage/quick_add_storage.go internal/handlers/quick_add_handler.go internal/handlers/quick_add_handler_test.go internal/routes/quick_add_routes.go
git commit -m "feat(api): add POST /quick-add creating category, collection and item atomically

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Frontend — modelos e `QuickAddService`

**Files:**
- Create: `src/app/models/quick-add.model.ts`
- Create: `src/app/services/quick-add.service.ts`

**Interfaces:**
- Consumes: `BinaryObjectPayload` (`models/collection.model.ts`), `Item` (`models/item.model.ts`), `environment.apiBase`.
- Produces:
  ```ts
  export interface ResolvedRef { id: number; name: string; is_new: boolean }
  export interface QuickAddAnalysis { name: string; description: string; tags: string[]; category: ResolvedRef; collection: ResolvedRef }
  export type QuickAddRef = { id: number } | { new_name: string };
  export interface QuickAddRequest { category: QuickAddRef; collection: QuickAddRef; item: { name: string; description: string | null; tags: string[] | null; price: number; binary_object: BinaryObjectPayload | null } }
  class QuickAddService { analyze(imageBase64: string): Observable<QuickAddAnalysis>; create(req: QuickAddRequest): Observable<Item> }
  ```

- [ ] **Step 1: Create branch**

```bash
cd C:/Users/joaop/Dev/a3/collection-manager-frontend && git switch feature/ai-suggest && git switch -c feature/quick-add
```

- [ ] **Step 2: Create `src/app/models/quick-add.model.ts`**

```ts
import { BinaryObjectPayload } from './collection.model';

export interface ResolvedRef {
  id: number;
  name: string;
  is_new: boolean;
}

export interface QuickAddAnalysis {
  name: string;
  description: string;
  tags: string[];
  category: ResolvedRef;
  collection: ResolvedRef;
}

export type QuickAddRef = { id: number } | { new_name: string };

export interface QuickAddRequest {
  category: QuickAddRef;
  collection: QuickAddRef;
  item: {
    name: string;
    description: string | null;
    tags: string[] | null;
    price: number;
    binary_object: BinaryObjectPayload | null;
  };
}
```

- [ ] **Step 3: Create `src/app/services/quick-add.service.ts`**

```ts
import { Injectable } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';
import { environment } from '../../environments/environment';
import { Item } from '../models/item.model';
import { QuickAddAnalysis, QuickAddRequest } from '../models/quick-add.model';

@Injectable({ providedIn: 'root' })
export class QuickAddService {
  private readonly API_BASE = environment.apiBase;

  constructor(private http: HttpClient) {}

  analyze(imageBase64: string): Observable<QuickAddAnalysis> {
    return this.http.post<QuickAddAnalysis>(`${this.API_BASE}/quick-add/analyze`, {
      image_base64: imageBase64,
    });
  }

  create(request: QuickAddRequest): Observable<Item> {
    return this.http.post<Item>(`${this.API_BASE}/quick-add`, request);
  }
}
```

- [ ] **Step 4: Build** — `npx ng build` → Expected: sucesso (só o aviso de budget de `app.scss`, pré-existente). Sem teste próprio: são tipos e chamadas HTTP diretas, exercitados pelo spec da Task 6 e pelo E2E.

- [ ] **Step 5: Commit**

```bash
git add src/app/models/quick-add.model.ts src/app/services/quick-add.service.ts
git commit -m "feat(quick-add): add quick-add models and service

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Frontend — `QuickAddModal`

**Files:**
- Create: `src/app/features/quick-add/quick-add-modal.ts`, `quick-add-modal.html`, `quick-add-modal.scss`
- Test: `src/app/features/quick-add/quick-add-modal.spec.ts`

**Interfaces:**
- Consumes: `QuickAddService` (Task 5); `CategoryService.getCategories()` (`services/category.ts`); `CollectionService.getCollections()`; `AlertService.error()`; `Router`; `tagColor` (`shared/utils/tag-color`); `Category`, `Collection`, `BinaryObjectPayload`.
- Produces: componente standalone `QuickAddModal` (selector `app-quick-add-modal`) com `@Output() onClose: EventEmitter<void>`. Constante exportada `NEW_REF = 0`.

- [ ] **Step 1: Write the failing spec** — `quick-add-modal.spec.ts`:

```ts
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { of, Subject, throwError } from 'rxjs';
import { QuickAddModal, NEW_REF } from './quick-add-modal';
import { QuickAddService } from '../../services/quick-add.service';
import { CategoryService } from '../../services/category';
import { CollectionService } from '../../services/collection.service';
import { AlertService } from '../../services/alert.service';
import { QuickAddAnalysis } from '../../models/quick-add.model';

const categories = [
  { id: 1, name: 'Numismática' },
  { id: 2, name: 'Miniaturas' },
];
const collections = [
  { id: 10, name: 'Moedas antigas', category_id: 1, category: categories[0] },
  { id: 20, name: 'Hot Wheels', category_id: 2, category: categories[1] },
];
const photo = { base64: 'aW1n', filename: 'a.jpg', extension: 'jpg' };

function analysis(partial: Partial<QuickAddAnalysis> = {}): QuickAddAnalysis {
  return {
    name: 'Moeda de 1 real',
    description: 'Moeda prateada.',
    tags: ['moeda'],
    category: { id: 1, name: 'Numismática', is_new: false },
    collection: { id: 0, name: 'Moedas brasileiras', is_new: true },
    ...partial,
  };
}

describe('QuickAddModal', () => {
  let analyze: ReturnType<typeof vi.fn>;
  let create: ReturnType<typeof vi.fn>;
  let alertError: ReturnType<typeof vi.fn>;
  let navigate: ReturnType<typeof vi.fn>;
  let navigateByUrl: ReturnType<typeof vi.fn>;

  function setup(cats = categories, cols = collections): QuickAddModal {
    TestBed.configureTestingModule({
      imports: [QuickAddModal],
      providers: [
        { provide: QuickAddService, useValue: { analyze, create } },
        { provide: CategoryService, useValue: { getCategories: () => of(cats) } },
        { provide: CollectionService, useValue: { getCollections: () => of(cols) } },
        { provide: AlertService, useValue: { error: alertError } },
        { provide: Router, useValue: { navigate, navigateByUrl } },
      ],
    });
    const fixture = TestBed.createComponent(QuickAddModal);
    fixture.detectChanges();
    return fixture.componentInstance;
  }

  beforeEach(() => {
    analyze = vi.fn();
    create = vi.fn();
    alertError = vi.fn();
    navigate = vi.fn().mockResolvedValue(true);
    navigateByUrl = vi.fn().mockResolvedValue(true);
  });

  it('preenche a revisão com a análise da IA', () => {
    analyze.mockReturnValue(of(analysis()));
    const modal = setup();
    modal.photo.set(photo);

    modal.analyze();

    expect(analyze).toHaveBeenCalledWith('aW1n');
    expect(modal.step()).toBe('review');
    expect(modal.name()).toBe('Moeda de 1 real');
    expect(modal.description()).toBe('Moeda prateada.');
    expect(modal.tags()).toEqual(['moeda']);
    expect(modal.price()).toBe(0);
    expect(modal.categoryChoice()).toBe(1);
    expect(modal.collectionChoice()).toBe(NEW_REF);
    expect(modal.newCollectionName()).toBe('Moedas brasileiras');
  });

  it('fica em "analyzing" enquanto espera e não fecha', () => {
    const pending = new Subject<QuickAddAnalysis>();
    analyze.mockReturnValue(pending);
    const modal = setup();
    const closed = vi.fn();
    modal.onClose.subscribe(closed);
    modal.photo.set(photo);

    modal.analyze();
    modal.close();

    expect(modal.step()).toBe('analyzing');
    expect(closed).not.toHaveBeenCalled();
  });

  it('volta para o upload mantendo a foto quando a análise falha', () => {
    analyze.mockReturnValue(throwError(() => new Error('503')));
    const modal = setup();
    modal.photo.set(photo);

    modal.analyze();

    expect(alertError).toHaveBeenCalled();
    expect(modal.step()).toBe('upload');
    expect(modal.photo()).toEqual(photo);
  });

  it('filtra coleções pela categoria escolhida', () => {
    const modal = setup();
    modal.onCategoryChange(2);
    expect(modal.filteredCollections().map((c) => c.id)).toEqual([20]);
  });

  it('troca a coleção para nova quando ela não pertence à categoria escolhida', () => {
    analyze.mockReturnValue(of(analysis({ collection: { id: 10, name: 'Moedas antigas', is_new: false } })));
    const modal = setup();
    modal.photo.set(photo);
    modal.analyze();

    modal.onCategoryChange(2);

    expect(modal.collectionChoice()).toBe(NEW_REF);
  });

  it('categoria nova não oferece coleções existentes', () => {
    const modal = setup();
    modal.onCategoryChange(NEW_REF);
    expect(modal.filteredCollections()).toEqual([]);
    expect(modal.collectionChoice()).toBe(NEW_REF);
  });

  it('sem categorias/coleções, a revisão começa com tudo novo', () => {
    analyze.mockReturnValue(
      of(analysis({ category: { id: 0, name: 'Selos', is_new: true }, collection: { id: 0, name: 'Selos', is_new: true } })),
    );
    const modal = setup([], []);
    modal.photo.set(photo);
    modal.analyze();

    expect(modal.categoryChoice()).toBe(NEW_REF);
    expect(modal.newCategoryName()).toBe('Selos');
    expect(modal.filteredCollections()).toEqual([]);
  });

  it('envia {id} para existentes e {new_name} para novos e navega recarregando a rota', async () => {
    analyze.mockReturnValue(of(analysis()));
    create.mockReturnValue(of({ id: 5, collection_id: 42 }));
    const modal = setup();
    const closed = vi.fn();
    modal.onClose.subscribe(closed);
    modal.photo.set(photo);
    modal.analyze();
    modal.price.set(12.5);

    modal.save();
    await Promise.resolve();

    expect(create).toHaveBeenCalledWith({
      category: { id: 1 },
      collection: { new_name: 'Moedas brasileiras' },
      item: {
        name: 'Moeda de 1 real',
        description: 'Moeda prateada.',
        tags: ['moeda'],
        price: 12.5,
        binary_object: photo,
      },
    });
    expect(closed).toHaveBeenCalled();
    expect(navigateByUrl).toHaveBeenCalledWith('/', { skipLocationChange: true });
    expect(navigate).toHaveBeenCalledWith(['/collections', 42, 'items']);
  });

  it('não envia com nome vazio ou nome de categoria nova vazio', () => {
    analyze.mockReturnValue(of(analysis()));
    const modal = setup();
    modal.photo.set(photo);
    modal.analyze();

    modal.name.set('  ');
    modal.save();
    modal.name.set('ok');
    modal.onCategoryChange(NEW_REF);
    modal.newCategoryName.set(' ');
    modal.save();

    expect(create).not.toHaveBeenCalled();
    expect(alertError).toHaveBeenCalledTimes(2);
  });

  it('mostra erro e permanece na revisão quando o cadastro falha', () => {
    analyze.mockReturnValue(of(analysis()));
    create.mockReturnValue(throwError(() => new Error('500')));
    const modal = setup();
    modal.photo.set(photo);
    modal.analyze();

    modal.save();

    expect(alertError).toHaveBeenCalled();
    expect(modal.step()).toBe('review');
    expect(modal.isSaving()).toBe(false);
  });
});
```

- [ ] **Step 2: Run to verify failure**

Run: `npx ng test --watch=false`
Expected: erro de compilação — `Cannot find module './quick-add-modal'`.

- [ ] **Step 3: Implement `quick-add-modal.ts`**

```ts
import { Component, EventEmitter, OnInit, Output, computed, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { BinaryObjectPayload, Collection } from '../../models/collection.model';
import { Category } from '../../models/category.model';
import { QuickAddAnalysis, QuickAddRef, QuickAddRequest } from '../../models/quick-add.model';
import { QuickAddService } from '../../services/quick-add.service';
import { CategoryService } from '../../services/category';
import { CollectionService } from '../../services/collection.service';
import { AlertService } from '../../services/alert.service';
import { tagColor } from '../../shared/utils/tag-color';

/** Valor usado nos selects para "criar nova". */
export const NEW_REF = 0;

type Step = 'upload' | 'analyzing' | 'review';

@Component({
  standalone: true,
  selector: 'app-quick-add-modal',
  imports: [CommonModule, FormsModule],
  templateUrl: './quick-add-modal.html',
  styleUrl: './quick-add-modal.scss',
})
export class QuickAddModal implements OnInit {
  @Output() onClose = new EventEmitter<void>();

  readonly NEW_REF = NEW_REF;

  step = signal<Step>('upload');
  photo = signal<BinaryObjectPayload | null>(null);
  previewUrl = signal<string | null>(null);

  categories = signal<Category[]>([]);
  collections = signal<Collection[]>([]);

  categoryChoice = signal<number>(NEW_REF);
  newCategoryName = signal('');
  collectionChoice = signal<number>(NEW_REF);
  newCollectionName = signal('');

  name = signal('');
  description = signal('');
  price = signal(0);
  tags = signal<string[]>([]);
  tagInput = signal('');
  isSaving = signal(false);

  filteredCollections = computed(() => {
    const cat = this.categoryChoice();
    if (cat === NEW_REF) return [];
    return this.collections().filter((c) => c.category_id === cat);
  });

  tagStyle = (tag: string) => tagColor(tag);

  constructor(
    private quickAddService: QuickAddService,
    private categoryService: CategoryService,
    private collectionService: CollectionService,
    private alertService: AlertService,
    private router: Router,
  ) {}

  ngOnInit(): void {
    this.categoryService.getCategories().subscribe({
      next: (cats) => this.categories.set(cats),
      error: () => this.categories.set([]),
    });
    this.collectionService.getCollections().subscribe({
      next: (cols) => this.collections.set(cols),
      error: () => this.collections.set([]),
    });
  }

  close(): void {
    if (this.step() === 'analyzing' || this.isSaving()) return;
    this.onClose.emit();
  }

  onFileSelected(event: Event): void {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;

    const reader = new FileReader();
    reader.onload = () => {
      const result = reader.result as string;
      const base64 = result.split(',')[1] ?? '';
      const dotIdx = file.name.lastIndexOf('.');
      const extension = dotIdx >= 0 ? file.name.slice(dotIdx + 1).toLowerCase() : '';
      this.photo.set({ base64, filename: file.name, extension });
      this.previewUrl.set(result);
    };
    reader.onerror = () => this.alertService.error('Não foi possível ler o arquivo selecionado.');
    reader.readAsDataURL(file);
  }

  analyze(): void {
    const photo = this.photo();
    if (!photo) return;

    this.step.set('analyzing');
    this.quickAddService.analyze(photo.base64).subscribe({
      next: (analysis) => {
        this.applyAnalysis(analysis);
        this.step.set('review');
      },
      error: () => {
        this.alertService.error('Não foi possível analisar a imagem. Verifique se a IA está disponível.');
        this.step.set('upload');
      },
    });
  }

  private applyAnalysis(a: QuickAddAnalysis): void {
    this.name.set(a.name);
    this.description.set(a.description);
    this.tags.set([...a.tags]);
    this.price.set(0);
    this.categoryChoice.set(a.category.is_new ? NEW_REF : a.category.id);
    this.newCategoryName.set(a.category.is_new ? a.category.name : '');
    this.collectionChoice.set(a.collection.is_new ? NEW_REF : a.collection.id);
    this.newCollectionName.set(a.collection.is_new ? a.collection.name : '');
  }

  onCategoryChange(value: number): void {
    this.categoryChoice.set(Number(value));
    const stillValid = this.filteredCollections().some((c) => c.id === this.collectionChoice());
    if (!stillValid) {
      this.collectionChoice.set(NEW_REF);
    }
  }

  onCollectionChange(value: number): void {
    this.collectionChoice.set(Number(value));
  }

  addTagFromInput(): void {
    const parts = this.tagInput().split(',').map((p) => p.trim()).filter(Boolean);
    if (parts.length === 0) return;
    this.tags.update((current) => {
      const next = [...current];
      for (const p of parts) if (!next.includes(p)) next.push(p);
      return next;
    });
    this.tagInput.set('');
  }

  onTagKeydown(event: KeyboardEvent): void {
    if (event.key === 'Enter' || event.key === ',') {
      event.preventDefault();
      this.addTagFromInput();
    }
  }

  removeTag(tag: string): void {
    this.tags.update((current) => current.filter((t) => t !== tag));
  }

  save(): void {
    const request = this.buildRequest();
    if (!request) return;

    this.isSaving.set(true);
    this.quickAddService.create(request).subscribe({
      next: (item) => {
        this.isSaving.set(false);
        this.onClose.emit();
        // ItemList lê o collectionId via snapshot; passar por '/' força a recriação da rota.
        this.router
          .navigateByUrl('/', { skipLocationChange: true })
          .then(() => this.router.navigate(['/collections', item.collection_id, 'items']));
      },
      error: () => {
        this.alertService.error('Erro ao cadastrar item.');
        this.isSaving.set(false);
      },
    });
  }

  private buildRequest(): QuickAddRequest | null {
    const name = this.name().trim();
    if (!name) {
      this.alertService.error('Nome não pode ser vazio.');
      return null;
    }
    const price = Number(this.price());
    if (isNaN(price) || price < 0) {
      this.alertService.error('Valor inválido.');
      return null;
    }

    const category = this.toRef(this.categoryChoice(), this.newCategoryName());
    const collection = this.toRef(this.collectionChoice(), this.newCollectionName());
    if (!category || !collection) {
      this.alertService.error('Informe o nome da nova categoria/coleção.');
      return null;
    }

    const description = this.description().trim();
    const tags = this.tags();
    return {
      category,
      collection,
      item: {
        name,
        description: description === '' ? null : description,
        tags: tags.length === 0 ? null : tags,
        price,
        binary_object: this.photo(),
      },
    };
  }

  private toRef(choice: number, newName: string): QuickAddRef | null {
    if (choice !== NEW_REF) return { id: choice };
    const trimmed = newName.trim();
    return trimmed ? { new_name: trimmed } : null;
  }
}
```

- [ ] **Step 4: Implement `quick-add-modal.html`**

```html
<div class="modal-overlay" (click)="close()">
  <div class="modal-content" (click)="$event.stopPropagation()">
    <div class="modal-header">
      <h2>✨ Cadastro rápido</h2>
      <button class="btn-close" (click)="close()" [disabled]="step() === 'analyzing' || isSaving()">&times;</button>
    </div>

    <div class="modal-body">
      @if (step() !== 'review') {
        <p class="hint">Envie uma foto do item. A IA sugere nome, descrição, tags, categoria e coleção.</p>
        <div class="form-group">
          <label for="quick-add-photo">Foto</label>
          <input
            type="file"
            id="quick-add-photo"
            accept="image/*"
            (change)="onFileSelected($event)"
            [disabled]="step() === 'analyzing'"
          />
        </div>
        @if (previewUrl()) {
          <div class="image-preview">
            <img [src]="previewUrl()" alt="Prévia" />
          </div>
        }
        @if (step() === 'analyzing') {
          <div class="analyzing">
            <span class="spinner" aria-hidden="true"></span>
            Analisando imagem... (pode levar alguns segundos)
          </div>
        }
      } @else {
        <div class="review-header">
          @if (previewUrl()) {
            <img [src]="previewUrl()" alt="Prévia" />
          }
          <p class="hint">Revise as sugestões antes de cadastrar.</p>
        </div>

        <div class="form-group">
          <label for="quick-add-category">
            Categoria
            <span class="badge" [class.badge-new]="categoryChoice() === NEW_REF">
              {{ categoryChoice() === NEW_REF ? 'nova' : 'existente' }}
            </span>
          </label>
          <select
            id="quick-add-category"
            [ngModel]="categoryChoice()"
            (ngModelChange)="onCategoryChange($event)"
            [disabled]="isSaving()"
          >
            @for (cat of categories(); track cat.id) {
              <option [ngValue]="cat.id">{{ cat.name }}</option>
            }
            <option [ngValue]="NEW_REF">➕ Nova categoria</option>
          </select>
          @if (categoryChoice() === NEW_REF) {
            <input
              type="text"
              [ngModel]="newCategoryName()"
              (ngModelChange)="newCategoryName.set($event)"
              [disabled]="isSaving()"
              placeholder="Nome da nova categoria"
            />
          }
        </div>

        <div class="form-group">
          <label for="quick-add-collection">
            Coleção
            <span class="badge" [class.badge-new]="collectionChoice() === NEW_REF">
              {{ collectionChoice() === NEW_REF ? 'nova' : 'existente' }}
            </span>
          </label>
          <select
            id="quick-add-collection"
            [ngModel]="collectionChoice()"
            (ngModelChange)="onCollectionChange($event)"
            [disabled]="isSaving()"
          >
            @for (col of filteredCollections(); track col.id) {
              <option [ngValue]="col.id">{{ col.name }}</option>
            }
            <option [ngValue]="NEW_REF">➕ Nova coleção</option>
          </select>
          @if (collectionChoice() === NEW_REF) {
            <input
              type="text"
              [ngModel]="newCollectionName()"
              (ngModelChange)="newCollectionName.set($event)"
              [disabled]="isSaving()"
              placeholder="Nome da nova coleção"
            />
          }
        </div>

        <div class="form-group">
          <label for="quick-add-name">Nome</label>
          <input id="quick-add-name" type="text" [ngModel]="name()" (ngModelChange)="name.set($event)" [disabled]="isSaving()" />
        </div>

        <div class="form-group">
          <label for="quick-add-description">Descrição</label>
          <textarea
            id="quick-add-description"
            rows="3"
            [ngModel]="description()"
            (ngModelChange)="description.set($event)"
            [disabled]="isSaving()"
          ></textarea>
        </div>

        <div class="form-group">
          <label for="quick-add-price">Valor (R$)</label>
          <input
            id="quick-add-price"
            type="number"
            step="0.01"
            min="0"
            [ngModel]="price()"
            (ngModelChange)="price.set($event)"
            [disabled]="isSaving()"
          />
        </div>

        <div class="form-group">
          <label for="quick-add-tag-input">Tags</label>
          <div class="tag-input-row">
            <input
              id="quick-add-tag-input"
              type="text"
              [ngModel]="tagInput()"
              (ngModelChange)="tagInput.set($event)"
              (keydown)="onTagKeydown($event)"
              [disabled]="isSaving()"
              placeholder="Digite uma tag e pressione Enter"
            />
            <button type="button" class="btn-secondary" (click)="addTagFromInput()" [disabled]="isSaving() || !tagInput().trim()">
              Adicionar
            </button>
          </div>
          @if (tags().length > 0) {
            <div class="tag-list">
              @for (tag of tags(); track tag) {
                <span
                  class="tag-chip"
                  [style.background]="tagStyle(tag).background"
                  [style.color]="tagStyle(tag).color"
                  [style.borderColor]="tagStyle(tag).border"
                >
                  {{ tag }}
                  <button type="button" class="tag-remove" (click)="removeTag(tag)" [disabled]="isSaving()" aria-label="Remover tag">&times;</button>
                </span>
              }
            </div>
          }
        </div>
      }
    </div>

    <div class="modal-footer">
      <button class="btn-secondary" (click)="close()" [disabled]="step() === 'analyzing' || isSaving()">Cancelar</button>
      @if (step() !== 'review') {
        <button class="btn-primary" (click)="analyze()" [disabled]="!photo() || step() === 'analyzing'">
          {{ step() === 'analyzing' ? 'Analisando...' : 'Analisar com IA' }}
        </button>
      } @else {
        <button class="btn-primary" (click)="save()" [disabled]="isSaving()">
          {{ isSaving() ? 'Cadastrando...' : 'Cadastrar' }}
        </button>
      }
    </div>
  </div>
</div>
```

- [ ] **Step 5: Implement `quick-add-modal.scss`** — reaproveita o estilo do modal de item e adiciona os elementos novos:

```scss
@use '../items/item-modal.scss';

select {
  padding: 10px 12px;
  border: 1px solid var(--border-strong);
  border-radius: 6px;
  font-size: 0.875rem;
  width: 100%;
  background: var(--input-bg);
  color: var(--text);
  font-family: inherit;

  &:focus {
    outline: none;
    border-color: var(--input-focus);
  }
}

.hint {
  margin: 0;
  font-size: 0.875rem;
  color: var(--text-muted);
}

.analyzing {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 0.875rem;
  color: var(--text-muted);
}

.spinner {
  width: 18px;
  height: 18px;
  border: 2px solid var(--border-strong);
  border-top-color: var(--input-focus);
  border-radius: 50%;
  animation: quick-add-spin 0.8s linear infinite;
}

@keyframes quick-add-spin {
  to {
    transform: rotate(360deg);
  }
}

.review-header {
  display: flex;
  align-items: center;
  gap: 12px;

  img {
    width: 64px;
    height: 64px;
    object-fit: cover;
    border-radius: 6px;
    border: 1px solid var(--border);
  }
}

.badge {
  margin-left: 6px;
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 0.6875rem;
  font-weight: 600;
  background: var(--surface-muted);
  color: var(--text-muted);

  &.badge-new {
    background: rgba(99, 102, 241, 0.15);
    color: #6366f1;
  }
}
```

  Se o `@use` do scss do item-modal falhar no build (escopo de estilos de componente não permite `@use` de arquivo de outro componente com seletores), copiar as regras `.modal-*`, `.form-group`, `.image-preview`, `.tag-*` e o `@media (max-width: 480px)` de `item-modal.scss` para este arquivo.

- [ ] **Step 6: Run tests**

Run: `npx ng test --watch=false`
Expected: 10 testes novos de `QuickAddModal` + 5 de `ItemModal` passam; apenas os 2 de `app.spec.ts` falham (pré-existentes).

- [ ] **Step 7: Commit**

```bash
git add src/app/features/quick-add
git commit -m "feat(quick-add): add quick-add modal with AI analysis and review

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Frontend — botão no header

**Files:**
- Modify: `src/app/app.ts`, `src/app/app.html`, `src/app/app.scss`

**Interfaces:**
- Consumes: `QuickAddModal` (Task 6), `currentUser` (já existe no `App`).
- Produces: signal `quickAddOpen` e métodos `openQuickAdd()`/`closeQuickAdd()` no `App`.

- [ ] **Step 1: `app.ts`** — importar e registrar o componente, e adicionar o estado:

```ts
import { QuickAddModal } from './features/quick-add/quick-add-modal';
// imports: [RouterOutlet, RouterLink, RouterLinkActive, CommonModule, ConfirmationModal, AlertModal, QuickAddModal],

  quickAddOpen = signal(false);

  openQuickAdd(): void {
    this.closeDrawer();
    this.quickAddOpen.set(true);
  }

  closeQuickAdd(): void {
    this.quickAddOpen.set(false);
  }
```

- [ ] **Step 2: `app.html`** — dentro de `<div class="header-actions">`, antes do botão `theme-toggle`:

```html
      @if (currentUser()) {
        <button
          type="button"
          class="quick-add-btn"
          (click)="openQuickAdd()"
          aria-label="Cadastro rápido com IA"
          title="Cadastro rápido com IA"
        >
          ✨ <span class="quick-add-label">Cadastro rápido</span>
        </button>
      }
```

  e, dentro de `<section class="page-content">`, após `<app-alert-modal>`:

```html
    @if (quickAddOpen()) {
      <app-quick-add-modal (onClose)="closeQuickAdd()"></app-quick-add-modal>
    }
```

- [ ] **Step 3: `app.scss`** — dentro de `.app-shell`, após o bloco `.theme-toggle { ... }`:

```scss
  .quick-add-btn {
    background: rgba(255, 255, 255, 0.12);
    border: 1px solid var(--header-divider);
    color: var(--header-text);
    height: 36px;
    padding: 0 0.9rem;
    border-radius: 999px;
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    font-size: 0.875rem;
    font-weight: 600;
    cursor: pointer;
    transition: background-color 0.2s, border-color 0.2s;

    &:hover {
      background: rgba(255, 255, 255, 0.2);
      border-color: rgba(255, 255, 255, 0.35);
    }
  }
```

  e dentro do `@media (max-width: 480px)` existente:

```scss
    .quick-add-btn {
      width: 36px;
      padding: 0;
      justify-content: center;
    }

    .quick-add-label {
      display: none;
    }
```

- [ ] **Step 4: Build and test**

Run: `npx ng build && npx ng test --watch=false`
Expected: build ok (o aviso de budget de `app.scss` pode crescer — continua apenas aviso; se virar erro, mover os estilos do botão para `src/styles.scss`); testes como na Task 6.

- [ ] **Step 5: Commit**

```bash
git add src/app/app.ts src/app/app.html src/app/app.scss
git commit -m "feat(quick-add): add quick-add button to the header

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Verificação E2E e entrega

- [ ] **Step 1:** Com Postgres (`collection-manager-db`), Ollama, backend (`go run ./cmd/api`) e frontend (`npm start`) rodando, chamar `POST /quick-add/analyze` com uma foto real (base64) e conferir: resposta 200, `category`/`collection` coerentes com as existentes do admin (Numismática / Moedas brasileiras), tempo de resposta.
- [ ] **Step 2:** Pelo navegador (`http://localhost:4200`, admin): clicar "✨ Cadastro rápido", enviar foto, revisar, cadastrar; verificar que abre a coleção certa com o item. Repetir estando já dentro de outra coleção (Review Focus 1).
- [ ] **Step 3:** Rodar `go test ./internal/...` e `npx ng test --watch=false` uma última vez.
- [ ] **Step 4:** `git push -u origin feature/quick-add` nos dois repositórios e abrir PRs com base `feature/ai-suggest` (dependem dos PRs de sugestão), descrições terminando com `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.
