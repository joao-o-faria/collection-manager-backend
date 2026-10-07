# Busca semântica e tela Home — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tela Home com busca semântica de itens (embeddings `embeddinggemma` via Ollama + similaridade de cosseno em Go), sugestões clicáveis e painel de totais/últimos itens.

**Architecture:** Backend Go guarda o embedding de cada item em `items.embedding real[]` (tipo `models.Vector`), gerado em segundo plano por um `search.Indexer` ao salvar itens e no boot. `GET /search` gera o vetor da frase, ranqueia os vetores do usuário com funções puras de `internal/search` e cai para busca textual (ILIKE) se a IA falhar. `GET /home/summary` agrega totais, últimos itens e tags mais usadas. Frontend Angular ganha `HomeService` e o componente `Home`, que vira a página inicial.

**Tech Stack:** Go 1.25, Gin, GORM + pgx (Postgres 16, sem pgvector), Ollama `/api/embed`; Angular 21 (standalone, signals), Vitest via `ng test`.

**Spec:** `collection-manager-backend/docs/superpowers/specs/2026-10-07-busca-semantica-home-design.md`

## Global Constraints

- Backend: branch `feature/semantic-search` (já criado a partir de `develop`) em `C:\Users\joaop\Dev\a3\collection-manager-backend`.
- Frontend: criar `feature/semantic-search` a partir de `develop` em `C:\Users\joaop\Dev\a3\collection-manager-frontend`.
- Modelo de embedding: `OLLAMA_EMBED_MODEL`, padrão `embeddinggemma`. Prefixos: documento `"title: none | text: "`, consulta `"task: search result | query: "`.
- `MinScore = 0.35`, `Limit = 12` (constantes em `internal/search`).
- Escopo de dados: **só o próprio usuário** (`user_id = ?`), inclusive admin — mesma regra do cadastro rápido.
- Postgres entrega `real[]` como string `{0.1,-2.5,3e-07}` e `NULL` como `nil`; aceita string literal `{1,2.5}` ao gravar (verificado contra o banco local).
- IA indisponível nunca quebra: salvar item funciona sem vetor; `/search` cai para texto.
- Textos de UI em pt-BR. Locale `pt-BR` já registrado no Angular (`CurrencyPipe` com `'BRL'` funciona).
- Commits terminam com `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Testes: backend `go test ./internal/...`; frontend `npx ng test --watch=false` (os 2 testes de `app.spec.ts` já falham antes — fora do escopo).

## Review Focus

1. **Ollama ligado, mas `embeddinggemma` não baixado** (erro 404 do Ollama) — espera-se busca por texto funcionando, não erro. Coberto: Task 5 (fallback em qualquer erro do embed).
2. **Primeira busca logo após subir o backend, antes da indexação terminar (nenhum item com vetor)** — espera-se encontrar itens por texto, não lista vazia. Coberto: Task 5 (sem candidatos → fallback texto) com teste.
3. **Busca com `%` ou `_`** — espera-se tratar como caracteres literais. Coberto: Task 5 (`escapeLikePattern`) + verificação E2E.
4. **Item excluído entre o ranking e o carregamento** — espera-se que simplesmente não apareça. Coberto: Task 5 (`GetItemsByIDs` ignora ids ausentes) com teste do handler.
5. **Resumo da Home falha (backend fora/erro)** — espera-se que a busca continue utilizável. Coberto: Task 8 com teste.

---

## File Structure

**Backend**
- Create `migrations/000010_add_item_embedding.up.sql` / `.down.sql`.
- Create `internal/models/vector.go` (+ `vector_test.go`); Modify `internal/models/item.go`.
- Modify `internal/ai/ollama.go` (campo `embedModel`); Create `internal/ai/embed.go` (+ `embed_test.go`).
- Create `internal/search/search.go` (+ `search_test.go`) — `ItemText`, `Cosine`, `Rank`, `TopTags`, tipos.
- Create `internal/search/indexer.go` (+ `indexer_test.go`).
- Create `internal/storage/search_storage.go` — consultas de indexação, busca e resumo.
- Create `internal/handlers/index_hook.go`; Modify `item_handler.go`, `quick_add_handler.go`.
- Create `internal/handlers/search_handler.go` (+ test), `internal/handlers/home_handler.go` (+ test).
- Create `internal/routes/search_routes.go`, `internal/routes/home_routes.go`.
- Modify `cmd/api/main.go`, `.env.example`, `README.md`.

**Frontend**
- Create `src/app/models/home.model.ts`, `src/app/services/home.service.ts`; Modify `src/app/models/item.model.ts`.
- Create `src/app/features/home/home.ts`, `home.html`, `home.scss`, `home.spec.ts`.
- Modify `src/app/app.routes.ts`, `src/app/app.html`, `src/app/features/auth/login/login.ts`.

---

### Task 1: `models.Vector` + migration

**Files:**
- Create: `migrations/000010_add_item_embedding.up.sql`, `migrations/000010_add_item_embedding.down.sql`
- Create: `internal/models/vector.go`
- Modify: `internal/models/item.go`
- Test: `internal/models/vector_test.go`

**Interfaces:**
- Produces: `type Vector []float32` com `Value() (driver.Value, error)` e `Scan(any) error`; campo `models.Item.Embedding Vector` (`json:"-"`).

- [ ] **Step 1: Write the failing test** — `internal/models/vector_test.go`:

```go
package models

import (
	"encoding/json"
	"testing"
)

func TestVector_ValueFormatsPostgresArray(t *testing.T) {
	v, err := Vector{0.1, -2.5, 3e-07}.Value()
	if err != nil {
		t.Fatal(err)
	}
	if v != "{0.1,-2.5,3e-07}" {
		t.Errorf("value = %v", v)
	}
}

func TestVector_NilValueIsNull(t *testing.T) {
	v, err := Vector(nil).Value()
	if err != nil || v != nil {
		t.Errorf("value = %v, err = %v; want nil, nil", v, err)
	}
}

func TestVector_ScanStringAndBytes(t *testing.T) {
	for _, in := range []any{"{0.1,-2.5,3e-07}", []byte("{0.1,-2.5,3e-07}")} {
		var v Vector
		if err := v.Scan(in); err != nil {
			t.Fatalf("scan %T: %v", in, err)
		}
		if len(v) != 3 || v[0] != 0.1 || v[1] != -2.5 || v[2] != 3e-07 {
			t.Errorf("scan %T = %v", in, v)
		}
	}
}

func TestVector_ScanNullAndEmpty(t *testing.T) {
	var v Vector = Vector{1}
	if err := v.Scan(nil); err != nil || v != nil {
		t.Errorf("scan nil = %v, %v", v, err)
	}
	if err := v.Scan("{}"); err != nil || len(v) != 0 {
		t.Errorf("scan {} = %v, %v", v, err)
	}
}

func TestVector_ScanRejectsGarbage(t *testing.T) {
	var v Vector
	if err := v.Scan("{1,abc}"); err == nil {
		t.Error("want error for non-numeric element")
	}
	if err := v.Scan(42); err == nil {
		t.Error("want error for unsupported type")
	}
}

func TestItem_EmbeddingNotInJSON(t *testing.T) {
	b, err := json.Marshal(Item{Name: "x", Embedding: Vector{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["embedding"]; ok {
		t.Errorf("embedding leaked into JSON: %s", b)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/models/`
Expected: build failure — `undefined: Vector`, `unknown field Embedding`.

- [ ] **Step 3: Implement `internal/models/vector.go`**

```go
package models

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
)

// Vector é um embedding guardado numa coluna real[] do Postgres.
type Vector []float32

func (v Vector) Value() (driver.Value, error) {
	if v == nil {
		return nil, nil
	}
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = strconv.FormatFloat(float64(x), 'g', -1, 32)
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func (v *Vector) Scan(value any) error {
	var raw string
	switch val := value.(type) {
	case nil:
		*v = nil
		return nil
	case string:
		raw = val
	case []byte:
		raw = string(val)
	default:
		return fmt.Errorf("tipo não suportado para Vector: %T", value)
	}

	raw = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(raw), "{"), "}")
	if raw == "" {
		*v = Vector{}
		return nil
	}

	parts := strings.Split(raw, ",")
	out := make(Vector, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return fmt.Errorf("elemento inválido em Vector: %q", p)
		}
		out[i] = float32(f)
	}
	*v = out
	return nil
}
```

- [ ] **Step 4: Add the field** — em `internal/models/item.go`, após `BinaryObject`:

```go
	Embedding      Vector        `json:"-" gorm:"type:real[]"`
```

- [ ] **Step 5: Create migrations**

`migrations/000010_add_item_embedding.up.sql`:
```sql
ALTER TABLE items
    ADD COLUMN IF NOT EXISTS embedding REAL[];
```

`migrations/000010_add_item_embedding.down.sql`:
```sql
ALTER TABLE items
    DROP COLUMN IF EXISTS embedding;
```

- [ ] **Step 6: Run tests, build and apply migration**

Run: `go test ./internal/... && go vet ./... && go build ./...`
Expected: PASS.
Run: `migrate -source file://migrations -database "postgres://postgres:postgres@localhost:5432/collection_manager?sslmode=disable" up`
Expected: `10/u add_item_embedding`.

- [ ] **Step 7: Commit**

```bash
git add migrations/000010_add_item_embedding.up.sql migrations/000010_add_item_embedding.down.sql internal/models/vector.go internal/models/vector_test.go internal/models/item.go
git commit -m "feat(api): store item embeddings in a real[] column

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: `ai.EmbedDocument` / `ai.EmbedQuery`

**Files:**
- Modify: `internal/ai/ollama.go`
- Create: `internal/ai/embed.go`
- Test: `internal/ai/embed_test.go`

**Interfaces:**
- Consumes: `Client`, `ErrUnavailable` (`ollama.go`).
- Produces:
  ```go
  const DefaultEmbedModel = "embeddinggemma"
  func (c *Client) WithEmbedModel(name string) *Client
  func (c *Client) EmbedDocument(ctx context.Context, text string) ([]float32, error)
  func (c *Client) EmbedQuery(ctx context.Context, text string) ([]float32, error)
  ```

- [ ] **Step 1: Write the failing test** — `internal/ai/embed_test.go`:

```go
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type embedCapture struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

func newFakeEmbed(t *testing.T, got *embedCapture, reply string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("path = %q, want /api/embed", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(got)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
}

func TestEmbedDocument_UsesDocumentPrefixAndDefaultModel(t *testing.T) {
	var got embedCapture
	srv := newFakeEmbed(t, &got, `{"embeddings":[[0.5,-1.5]]}`, http.StatusOK)
	defer srv.Close()

	v, err := NewClient(srv.URL, "gemma4:12b").EmbedDocument(context.Background(), "Moeda de prata")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Model != "embeddinggemma" || got.Input != "title: none | text: Moeda de prata" {
		t.Errorf("request = %+v", got)
	}
	if len(v) != 2 || v[0] != 0.5 || v[1] != -1.5 {
		t.Errorf("vector = %v", v)
	}
}

func TestEmbedQuery_UsesQueryPrefixAndConfiguredModel(t *testing.T) {
	var got embedCapture
	srv := newFakeEmbed(t, &got, `{"embeddings":[[1]]}`, http.StatusOK)
	defer srv.Close()

	_, err := NewClient(srv.URL, "gemma4:12b").WithEmbedModel("nomic-embed-text").EmbedQuery(context.Background(), "coisas antigas")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Model != "nomic-embed-text" || got.Input != "task: search result | query: coisas antigas" {
		t.Errorf("request = %+v", got)
	}
}

func TestWithEmbedModel_IgnoresEmptyName(t *testing.T) {
	var got embedCapture
	srv := newFakeEmbed(t, &got, `{"embeddings":[[1]]}`, http.StatusOK)
	defer srv.Close()

	_, _ = NewClient(srv.URL, "gemma4:12b").WithEmbedModel("").EmbedQuery(context.Background(), "x")
	if got.Model != "embeddinggemma" {
		t.Errorf("model = %q, want embeddinggemma", got.Model)
	}
}

func TestEmbed_ErrUnavailableWithOllamaMessage(t *testing.T) {
	var got embedCapture
	srv := newFakeEmbed(t, &got, `{"error":"model \"embeddinggemma\" not found"}`, http.StatusNotFound)
	defer srv.Close()

	_, err := NewClient(srv.URL, "gemma4:12b").EmbedQuery(context.Background(), "x")
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v", err)
	}
}

func TestEmbed_ErrUnavailableWhenNoVector(t *testing.T) {
	var got embedCapture
	srv := newFakeEmbed(t, &got, `{"embeddings":[]}`, http.StatusOK)
	defer srv.Close()

	_, err := NewClient(srv.URL, "gemma4:12b").EmbedQuery(context.Background(), "x")
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ai/`
Expected: build failure — `EmbedDocument undefined`, `WithEmbedModel undefined`.

- [ ] **Step 3: Add the `embedModel` field** — em `internal/ai/ollama.go`, no struct `Client` adicionar `embedModel string` e em `NewClient` inicializar `embedModel: DefaultEmbedModel`:

```go
type Client struct {
	baseURL    string
	model      string
	embedModel string
	http       *http.Client
}

func NewClient(baseURL, model string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		model:      model,
		embedModel: DefaultEmbedModel,
		http:       &http.Client{Timeout: 90 * time.Second},
	}
}
```

- [ ] **Step 4: Create `internal/ai/embed.go`**

```go
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const DefaultEmbedModel = "embeddinggemma"

// Prefixos recomendados pelo embeddinggemma para documentos e consultas de busca.
const (
	documentPrefix = "title: none | text: "
	queryPrefix    = "task: search result | query: "
)

// WithEmbedModel define o modelo de embedding (nome vazio mantém o atual).
func (c *Client) WithEmbedModel(name string) *Client {
	if name = strings.TrimSpace(name); name != "" {
		c.embedModel = name
	}
	return c
}

func (c *Client) EmbedDocument(ctx context.Context, text string) ([]float32, error) {
	return c.embed(ctx, documentPrefix+text)
}

func (c *Client) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return c.embed(ctx, queryPrefix+text)
}

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

func (c *Client) embed(ctx context.Context, input string) ([]float32, error) {
	body, err := json.Marshal(embedRequest{Model: c.embedModel, Input: input})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("%w: status %d: %s", ErrUnavailable, resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	var out embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: resposta inválida: %v", ErrUnavailable, err)
	}
	if len(out.Embeddings) == 0 || len(out.Embeddings[0]) == 0 {
		return nil, fmt.Errorf("%w: resposta sem vetor", ErrUnavailable)
	}
	return out.Embeddings[0], nil
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/ai/`
Expected: PASS (testes antigos + 5 novos).

- [ ] **Step 6: Commit**

```bash
git add internal/ai/ollama.go internal/ai/embed.go internal/ai/embed_test.go
git commit -m "feat(ai): generate document and query embeddings with embeddinggemma

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: `internal/search` — funções puras

**Files:**
- Create: `internal/search/search.go`
- Test: `internal/search/search_test.go`

**Interfaces:**
- Consumes: `models.Item`, `models.Collection`, `models.Category`, `models.TagList`.
- Produces:
  ```go
  const MinScore float32 = 0.35
  const Limit = 12
  type Candidate struct { ItemID int; Vector []float32 }
  type Scored struct { ItemID int; Score float32 }
  func ItemText(item models.Item) string
  func Cosine(a, b []float32) float32
  func Rank(query []float32, candidates []Candidate, minScore float32, limit int) []Scored
  func TopTags(lists [][]string, n int) []string
  ```

- [ ] **Step 1: Write the failing test** — `internal/search/search_test.go`:

```go
package search

import (
	"fmt"
	"math"
	"testing"

	"collection-manager-backend/internal/models"
)

func TestItemText_IncludesAllPartsAndSkipsEmpty(t *testing.T) {
	desc := "Moeda prateada"
	item := models.Item{
		Name: "Moeda 1 real", Description: &desc, Tags: models.TagList{"moeda", "1998"},
		Collection: models.Collection{Name: "Moedas", Category: models.Category{Name: "Numismática"}},
	}
	want := "Moeda 1 real. Moeda prateada. Tags: moeda, 1998. Coleção: Moedas. Categoria: Numismática."
	if got := ItemText(item); got != want {
		t.Errorf("ItemText = %q\nwant      %q", got, want)
	}

	if got := ItemText(models.Item{Name: "Selo"}); got != "Selo." {
		t.Errorf("ItemText minimal = %q, want %q", got, "Selo.")
	}
}

func TestCosine(t *testing.T) {
	near := func(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-6 }
	if c := Cosine([]float32{1, 2, 3}, []float32{2, 4, 6}); !near(c, 1) {
		t.Errorf("parallel = %v, want 1", c)
	}
	if c := Cosine([]float32{1, 0}, []float32{0, 1}); !near(c, 0) {
		t.Errorf("orthogonal = %v, want 0", c)
	}
	if c := Cosine([]float32{1, 0}, []float32{-1, 0}); !near(c, -1) {
		t.Errorf("opposite = %v, want -1", c)
	}
	if c := Cosine([]float32{1, 2}, []float32{1, 2, 3}); c != 0 {
		t.Errorf("different sizes = %v, want 0", c)
	}
	if c := Cosine([]float32{0, 0}, []float32{1, 1}); c != 0 {
		t.Errorf("zero norm = %v, want 0", c)
	}
}

func TestRank_OrdersFiltersAndLimits(t *testing.T) {
	q := []float32{1, 0}
	cands := []Candidate{
		{ItemID: 1, Vector: []float32{0, 1}},     // 0
		{ItemID: 2, Vector: []float32{1, 0}},     // 1
		{ItemID: 3, Vector: []float32{1, 1}},     // ~0.707
		{ItemID: 4, Vector: []float32{1, 0.1}},   // ~0.995
		{ItemID: 5, Vector: []float32{1, 2, 3}},  // tamanho diferente → 0
	}

	got := Rank(q, cands, 0.5, 2)
	if fmt.Sprint(ids(got)) != "[2 4]" {
		t.Errorf("Rank ids = %v, want [2 4]", ids(got))
	}

	all := Rank(q, cands, 0.5, 10)
	if fmt.Sprint(ids(all)) != "[2 4 3]" {
		t.Errorf("Rank ids = %v, want [2 4 3]", ids(all))
	}
	if all[0].Score < all[1].Score || all[1].Score < all[2].Score {
		t.Errorf("not sorted: %+v", all)
	}
}

func TestRank_EmptyCandidates(t *testing.T) {
	if got := Rank([]float32{1}, nil, MinScore, Limit); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestTopTags_CountsThenAlphabetical(t *testing.T) {
	lists := [][]string{{"moeda", "prata"}, {"moeda", "brasil"}, {"prata", "moeda"}, {"azul"}}
	if got := TopTags(lists, 3); fmt.Sprint(got) != "[moeda prata azul]" {
		t.Errorf("TopTags = %v, want [moeda prata azul]", got)
	}
	if got := TopTags(nil, 5); len(got) != 0 {
		t.Errorf("TopTags(nil) = %v", got)
	}
}

func ids(s []Scored) []int {
	out := make([]int, len(s))
	for i, x := range s {
		out[i] = x.ItemID
	}
	return out
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/search/`
Expected: build failure — `undefined: ItemText`, `Cosine`, `Rank`, `TopTags`, `Candidate`.

- [ ] **Step 3: Implement `internal/search/search.go`**

```go
package search

import (
	"math"
	"sort"
	"strings"

	"collection-manager-backend/internal/models"
)

const (
	// MinScore é a similaridade mínima para um item aparecer na busca semântica.
	MinScore float32 = 0.35
	// Limit é o número máximo de resultados.
	Limit = 12
)

type Candidate struct {
	ItemID int
	Vector []float32
}

type Scored struct {
	ItemID int
	Score  float32
}

// ItemText monta o texto que representa o item para o embedding.
func ItemText(item models.Item) string {
	parts := []string{strings.TrimSpace(item.Name)}
	if item.Description != nil {
		parts = append(parts, strings.TrimSpace(*item.Description))
	}
	if len(item.Tags) > 0 {
		parts = append(parts, "Tags: "+strings.Join(item.Tags, ", "))
	}
	if item.Collection.Name != "" {
		parts = append(parts, "Coleção: "+item.Collection.Name)
	}
	if item.Collection.Category.Name != "" {
		parts = append(parts, "Categoria: "+item.Collection.Category.Name)
	}

	nonEmpty := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSuffix(p, "."); p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, ". ") + "."
}

// Cosine é a similaridade de cosseno entre dois vetores (0 se incompatíveis).
func Cosine(a, b []float32) float32 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

// Rank ordena os candidatos pela similaridade com a consulta, descartando os abaixo de minScore.
func Rank(query []float32, candidates []Candidate, minScore float32, limit int) []Scored {
	out := make([]Scored, 0, len(candidates))
	for _, c := range candidates {
		if s := Cosine(query, c.Vector); s >= minScore {
			out = append(out, Scored{ItemID: c.ItemID, Score: s})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// TopTags devolve as n tags mais frequentes (empates em ordem alfabética).
func TopTags(lists [][]string, n int) []string {
	counts := map[string]int{}
	for _, tags := range lists {
		for _, t := range tags {
			if t = strings.TrimSpace(t); t != "" {
				counts[t]++
			}
		}
	}
	tags := make([]string, 0, len(counts))
	for t := range counts {
		tags = append(tags, t)
	}
	sort.Slice(tags, func(i, j int) bool {
		if counts[tags[i]] != counts[tags[j]] {
			return counts[tags[i]] > counts[tags[j]]
		}
		return tags[i] < tags[j]
	})
	if len(tags) > n {
		tags = tags[:n]
	}
	return tags
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/search/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/search/search.go internal/search/search_test.go
git commit -m "feat(search): add cosine similarity ranking and item text helpers

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Indexação (indexer + storage + ganchos + boot)

**Files:**
- Create: `internal/search/indexer.go`
- Test: `internal/search/indexer_test.go`
- Create: `internal/storage/search_storage.go` (parte de indexação)
- Create: `internal/handlers/index_hook.go`
- Modify: `internal/handlers/item_handler.go`, `internal/handlers/quick_add_handler.go`
- Test: `internal/handlers/quick_add_handler_test.go` (acrescentar)
- Modify: `cmd/api/main.go`, `.env.example`, `README.md`

**Interfaces:**
- Consumes: `search.ItemText` (Task 3); `models.Vector` (Task 1); `ai.(*Client).EmbedDocument`, `WithEmbedModel` (Task 2).
- Produces:
  ```go
  // search
  type DocumentEmbedder interface { EmbedDocument(ctx context.Context, text string) ([]float32, error) }
  type IndexStore interface {
      GetItemForIndex(ctx context.Context, id int) (models.Item, error)
      SetItemEmbedding(ctx context.Context, id int, v models.Vector) error
      ItemsMissingEmbedding(ctx context.Context) ([]int, error)
  }
  func NewIndexer(e DocumentEmbedder, s IndexStore) *Indexer
  func (ix *Indexer) IndexItem(ctx context.Context, id int) error
  func (ix *Indexer) IndexItemAsync(id int)
  func (ix *Indexer) IndexMissing(ctx context.Context) (int, error)
  // storage
  type IndexStore struct{}   // implementa search.IndexStore
  // handlers
  func InitIndexer(fn func(itemID int))
  func notifyItemSaved(itemID int)
  ```

- [ ] **Step 1: Write the failing indexer tests** — `internal/search/indexer_test.go`:

```go
package search

import (
	"context"
	"errors"
	"testing"

	"collection-manager-backend/internal/models"
)

type fakeEmbedder struct {
	texts []string
	fail  map[string]bool
}

func (f *fakeEmbedder) EmbedDocument(_ context.Context, text string) ([]float32, error) {
	f.texts = append(f.texts, text)
	if f.fail[text] {
		return nil, errors.New("ollama fora")
	}
	return []float32{float32(len(text))}, nil
}

type fakeStore struct {
	items   map[int]models.Item
	saved   map[int]models.Vector
	missing []int
}

func (s *fakeStore) GetItemForIndex(_ context.Context, id int) (models.Item, error) {
	item, ok := s.items[id]
	if !ok {
		return models.Item{}, errors.New("não encontrado")
	}
	return item, nil
}

func (s *fakeStore) SetItemEmbedding(_ context.Context, id int, v models.Vector) error {
	s.saved[id] = v
	return nil
}

func (s *fakeStore) ItemsMissingEmbedding(context.Context) ([]int, error) {
	return s.missing, nil
}

func newFakes() (*fakeEmbedder, *fakeStore) {
	return &fakeEmbedder{fail: map[string]bool{}}, &fakeStore{
		items: map[int]models.Item{1: {ID: 1, Name: "Moeda"}, 2: {ID: 2, Name: "Selo"}, 3: {ID: 3, Name: "Carro"}},
		saved: map[int]models.Vector{},
	}
}

func TestIndexItem_EmbedsItemTextAndSaves(t *testing.T) {
	emb, store := newFakes()

	if err := NewIndexer(emb, store).IndexItem(context.Background(), 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(emb.texts) != 1 || emb.texts[0] != "Moeda." {
		t.Errorf("embedded texts = %v", emb.texts)
	}
	if v, ok := store.saved[1]; !ok || len(v) != 1 {
		t.Errorf("saved = %v", store.saved)
	}
}

func TestIndexItem_DoesNotSaveWhenEmbedFails(t *testing.T) {
	emb, store := newFakes()
	emb.fail["Moeda."] = true

	if err := NewIndexer(emb, store).IndexItem(context.Background(), 1); err == nil {
		t.Error("want error")
	}
	if len(store.saved) != 0 {
		t.Errorf("saved = %v, want nothing", store.saved)
	}
}

func TestIndexMissing_ContinuesPastFailuresAndCounts(t *testing.T) {
	emb, store := newFakes()
	store.missing = []int{1, 2, 3}
	emb.fail["Selo."] = true

	n, err := NewIndexer(emb, store).IndexMissing(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 2 || len(store.saved) != 2 || store.saved[2] != nil {
		t.Errorf("indexed = %d, saved = %v", n, store.saved)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/search/`
Expected: build failure — `undefined: NewIndexer`.

- [ ] **Step 3: Implement `internal/search/indexer.go`**

```go
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
```

- [ ] **Step 4: Run indexer tests**

Run: `go test ./internal/search/`
Expected: PASS.

- [ ] **Step 5: Create `internal/storage/search_storage.go`** (parte de indexação; Tasks 5 e 6 acrescentam funções ao mesmo arquivo):

```go
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
```

- [ ] **Step 6: Write the failing hook test** — acrescentar a `internal/handlers/quick_add_handler_test.go`:

```go
func TestCreateQuickAdd_NotifiesIndexerOnSuccess(t *testing.T) {
	var indexed []int
	prev := indexItem
	indexItem = func(id int) { indexed = append(indexed, id) }
	t.Cleanup(func() { indexItem = prev })

	r := setupCreate(t, func(context.Context, uint, bool, storage.QuickAddInput) (models.Item, error) {
		return models.Item{ID: 77, CollectionID: 1}, nil
	})
	w := post(r, "/quick-add", `{"category":{"id":1},"collection":{"id":10},"item":{"name":"x"}}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d", w.Code)
	}
	if len(indexed) != 1 || indexed[0] != 77 {
		t.Errorf("indexed = %v, want [77]", indexed)
	}
}

func TestCreateQuickAdd_DoesNotNotifyIndexerOnFailure(t *testing.T) {
	var indexed []int
	prev := indexItem
	indexItem = func(id int) { indexed = append(indexed, id) }
	t.Cleanup(func() { indexItem = prev })

	r := setupCreate(t, func(context.Context, uint, bool, storage.QuickAddInput) (models.Item, error) {
		return models.Item{}, storage.ErrNotFound
	})
	post(r, "/quick-add", `{"category":{"id":1},"collection":{"id":10},"item":{"name":"x"}}`)

	if len(indexed) != 0 {
		t.Errorf("indexed = %v, want none", indexed)
	}
}
```

Run: `go test ./internal/handlers/ -run Indexer`
Expected: build failure — `undefined: indexItem`.

- [ ] **Step 7: Create `internal/handlers/index_hook.go`**

```go
package handlers

// indexItem é chamado depois que um item é salvo, para gerar o embedding em segundo plano.
// Padrão: não faz nada (testes e execução sem IA).
var indexItem = func(itemID int) {}

// InitIndexer define a função que indexa um item salvo.
func InitIndexer(fn func(itemID int)) {
	if fn != nil {
		indexItem = fn
	}
}

func notifyItemSaved(itemID int) {
	indexItem(itemID)
}
```

- [ ] **Step 8: Call the hook** — imediatamente antes de cada resposta de sucesso:
  - `item_handler.go`, `CreateItem`: antes de `c.JSON(http.StatusCreated, item)` inserir `notifyItemSaved(item.ID)`.
  - `item_handler.go`, `UpdateItem`: antes de `c.JSON(http.StatusOK, item)` inserir `notifyItemSaved(item.ID)`.
  - `quick_add_handler.go`, `CreateQuickAdd`: antes de `c.JSON(http.StatusCreated, item)` inserir `notifyItemSaved(item.ID)`.

Run: `go test ./internal/...`
Expected: PASS.

- [ ] **Step 9: Wire in `cmd/api/main.go`** — substituir a criação do `aiClient` e acrescentar o indexer (adicionar imports `"context"` e `"collection-manager-backend/internal/search"`):

```go
	aiClient := ai.NewClient(
		envOrDefault("OLLAMA_URL", "http://localhost:11434"),
		envOrDefault("OLLAMA_MODEL", "gemma4:12b"),
	).WithEmbedModel(envOrDefault("OLLAMA_EMBED_MODEL", ai.DefaultEmbedModel))
	handlers.InitSuggester(aiClient)
	handlers.InitAnalyzer(aiClient)

	indexer := search.NewIndexer(aiClient, storage.IndexStore{})
	handlers.InitIndexer(indexer.IndexItemAsync)
	go func() {
		n, err := indexer.IndexMissing(context.Background())
		if err != nil {
			log.Printf("indexação inicial falhou: %v", err)
			return
		}
		log.Printf("indexação inicial: %d item(ns) indexado(s)", n)
	}()
```

- [ ] **Step 10: Document** — `.env.example`: acrescentar `OLLAMA_EMBED_MODEL=embeddinggemma`. `README.md`: nova seção ao final:

```markdown
## 🤖 IA local (Ollama)

Os recursos de IA usam o [Ollama](https://ollama.com) rodando localmente:

- `OLLAMA_URL` (padrão `http://localhost:11434`)
- `OLLAMA_MODEL` — modelo de chat/visão (padrão `gemma4:12b`)
- `OLLAMA_EMBED_MODEL` — modelo de embedding da busca semântica (padrão `embeddinggemma`)

Antes do primeiro uso, baixe os modelos:

```bash
ollama pull gemma4:12b
ollama pull embeddinggemma
```

Sem o Ollama, o sistema continua funcionando: a busca da Home usa busca por texto.
```

- [ ] **Step 11: Pull the model and verify the boot indexing against the real DB**

```bash
ollama pull embeddinggemma
go run ./cmd/api   # em outro terminal / background
```

Expected no log: `indexação inicial: 10 item(ns) indexado(s)` (ou o total de itens existentes). Conferir:

```bash
docker exec collection-manager-db psql -U postgres -d collection_manager -tAc "select count(*) filter (where embedding is null), count(*), max(array_length(embedding,1)) from items"
```

Expected: `0|<total>|768`.

- [ ] **Step 12: Commit**

```bash
git add internal/search/indexer.go internal/search/indexer_test.go internal/storage/search_storage.go internal/handlers/index_hook.go internal/handlers/item_handler.go internal/handlers/quick_add_handler.go internal/handlers/quick_add_handler_test.go cmd/api/main.go .env.example README.md
git commit -m "feat(api): index item embeddings on save and at startup

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: `GET /search`

**Files:**
- Modify: `internal/storage/search_storage.go` (acrescentar)
- Create: `internal/handlers/search_handler.go`
- Create: `internal/routes/search_routes.go`
- Modify: `cmd/api/main.go`
- Test: `internal/handlers/search_handler_test.go`

**Interfaces:**
- Consumes: `search.Rank`, `search.Candidate`, `search.MinScore`, `search.Limit` (Task 3); `ai.(*Client).EmbedQuery` (Task 2); `escapeLikePattern` (já existe em `category_storage.go`); `actorFromContext`.
- Produces:
  ```go
  // storage
  func ItemEmbeddings(ctx context.Context, userID uint) ([]search.Candidate, error)
  func GetItemsByIDs(ctx context.Context, userID uint, ids []int) ([]models.Item, error) // ordem de ids preservada, ausentes ignorados
  func SearchItemsText(ctx context.Context, userID uint, q string, limit int) ([]models.Item, error)
  // handlers
  type SearchResult struct { Item models.Item `json:"item"`; Score *float32 `json:"score"` }
  type SearchResponse struct { Mode string `json:"mode"`; Results []SearchResult `json:"results"` }
  func InitSearch(e queryEmbedder)
  func SearchItems(c *gin.Context)
  // routes
  func RegisterSearchRoutes(r *gin.Engine)
  ```

- [ ] **Step 1: Write the failing tests** — `internal/handlers/search_handler_test.go`:

```go
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"collection-manager-backend/internal/models"
	"collection-manager-backend/internal/search"

	"github.com/gin-gonic/gin"
)

type fakeQueryEmbedder struct {
	vec []float32
	err error
	got string
}

func (f *fakeQueryEmbedder) EmbedQuery(_ context.Context, q string) ([]float32, error) {
	f.got = q
	return f.vec, f.err
}

type searchFakes struct {
	embedder      *fakeQueryEmbedder
	candidates    []search.Candidate
	items         map[int]models.Item
	textResults   []models.Item
	gotUser       uint
	gotTextQuery  string
	gotIDs        []int
}

func setupSearch(t *testing.T, f *searchFakes, role models.Role, userID uint) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	prevE, prevC, prevI, prevT := queryEmbed, loadItemEmbeddings, loadItemsByIDs, searchItemsText
	if f.embedder != nil {
		queryEmbed = f.embedder
	} else {
		queryEmbed = nil
	}
	loadItemEmbeddings = func(_ context.Context, uid uint) ([]search.Candidate, error) {
		f.gotUser = uid
		return f.candidates, nil
	}
	loadItemsByIDs = func(_ context.Context, uid uint, ids []int) ([]models.Item, error) {
		f.gotIDs = ids
		var out []models.Item
		for _, id := range ids {
			if item, ok := f.items[id]; ok {
				out = append(out, item)
			}
		}
		return out, nil
	}
	searchItemsText = func(_ context.Context, uid uint, q string, limit int) ([]models.Item, error) {
		f.gotUser, f.gotTextQuery = uid, q
		return f.textResults, nil
	}
	t.Cleanup(func() { queryEmbed, loadItemEmbeddings, loadItemsByIDs, searchItemsText = prevE, prevC, prevI, prevT })

	r := gin.New()
	r.GET("/search", func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Set("user_role", role)
		SearchItems(c)
	})
	return r
}

func getSearch(r *gin.Engine, q string) (*httptest.ResponseRecorder, SearchResponse) {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/search?q="+url.QueryEscape(q), nil))
	var resp SearchResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w, resp
}

func resultIDs(resp SearchResponse) string {
	ids := make([]int, len(resp.Results))
	for i, r := range resp.Results {
		ids[i] = r.Item.ID
	}
	return fmt.Sprint(ids)
}

func TestSearchItems_SemanticRanksByCosine(t *testing.T) {
	f := &searchFakes{
		embedder: &fakeQueryEmbedder{vec: []float32{1, 0}},
		candidates: []search.Candidate{
			{ItemID: 1, Vector: []float32{0, 1}},   // 0 → abaixo do mínimo
			{ItemID: 2, Vector: []float32{1, 0.2}}, // ~0.98
			{ItemID: 3, Vector: []float32{1, 1}},   // ~0.71
		},
		items: map[int]models.Item{1: {ID: 1}, 2: {ID: 2}, 3: {ID: 3}},
	}
	r := setupSearch(t, f, models.UserRole, 7)

	w, resp := getSearch(r, "  coisas antigas ")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	if resp.Mode != "semantic" || resultIDs(resp) != "[2 3]" {
		t.Errorf("mode = %q ids = %s", resp.Mode, resultIDs(resp))
	}
	if resp.Results[0].Score == nil || *resp.Results[0].Score < *resp.Results[1].Score {
		t.Errorf("scores = %v, %v", resp.Results[0].Score, resp.Results[1].Score)
	}
	if f.embedder.got != "coisas antigas" || f.gotUser != 7 {
		t.Errorf("query = %q user = %d", f.embedder.got, f.gotUser)
	}
}

func TestSearchItems_SkipsItemsDeletedAfterRanking(t *testing.T) {
	f := &searchFakes{
		embedder:   &fakeQueryEmbedder{vec: []float32{1, 0}},
		candidates: []search.Candidate{{ItemID: 2, Vector: []float32{1, 0}}, {ItemID: 3, Vector: []float32{1, 0.1}}},
		items:      map[int]models.Item{3: {ID: 3}},
	}
	r := setupSearch(t, f, models.UserRole, 7)

	_, resp := getSearch(r, "x")

	if resultIDs(resp) != "[3]" {
		t.Errorf("ids = %s, want [3]", resultIDs(resp))
	}
}

func TestSearchItems_FallsBackToTextWhenEmbedFails(t *testing.T) {
	f := &searchFakes{
		embedder:    &fakeQueryEmbedder{err: errors.New("model not found")},
		candidates:  []search.Candidate{{ItemID: 1, Vector: []float32{1, 0}}},
		textResults: []models.Item{{ID: 9}},
	}
	r := setupSearch(t, f, models.UserRole, 7)

	w, resp := getSearch(r, "moeda")

	if w.Code != http.StatusOK || resp.Mode != "text" || resultIDs(resp) != "[9]" {
		t.Errorf("status = %d mode = %q ids = %s", w.Code, resp.Mode, resultIDs(resp))
	}
	if resp.Results[0].Score != nil {
		t.Errorf("score = %v, want null in text mode", *resp.Results[0].Score)
	}
	if f.gotTextQuery != "moeda" || f.embedder.got != "moeda" {
		t.Errorf("text query = %q, embed query = %q (embed must have been tried)", f.gotTextQuery, f.embedder.got)
	}
}

func TestSearchItems_FallsBackToTextWhenNoItemIsIndexedYet(t *testing.T) {
	f := &searchFakes{
		embedder:    &fakeQueryEmbedder{vec: []float32{1, 0}},
		candidates:  nil,
		textResults: []models.Item{{ID: 4}},
	}
	r := setupSearch(t, f, models.UserRole, 7)

	_, resp := getSearch(r, "moeda")

	if resp.Mode != "text" || resultIDs(resp) != "[4]" {
		t.Errorf("mode = %q ids = %s", resp.Mode, resultIDs(resp))
	}
}

func TestSearchItems_FallsBackToTextWithoutEmbedder(t *testing.T) {
	f := &searchFakes{textResults: []models.Item{}}
	r := setupSearch(t, f, models.UserRole, 7)

	w, resp := getSearch(r, "moeda")

	if w.Code != http.StatusOK || resp.Mode != "text" || resp.Results == nil {
		t.Errorf("status = %d mode = %q results = %v", w.Code, resp.Mode, resp.Results)
	}
}

func TestSearchItems_RejectsBlankQuery(t *testing.T) {
	r := setupSearch(t, &searchFakes{}, models.UserRole, 7)

	if w, _ := getSearch(r, "   "); w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSearchItems_AdminOnlySearchesOwnItems(t *testing.T) {
	f := &searchFakes{embedder: &fakeQueryEmbedder{vec: []float32{1}}, candidates: []search.Candidate{{ItemID: 1, Vector: []float32{1}}}, items: map[int]models.Item{1: {ID: 1}}}
	r := setupSearch(t, f, models.AdminRole, 1)

	getSearch(r, "x")

	if f.gotUser != 1 {
		t.Errorf("user = %d, want 1 (admin's own)", f.gotUser)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/handlers/ -run TestSearchItems`
Expected: build failure — `undefined: queryEmbed`, `loadItemEmbeddings`, `loadItemsByIDs`, `searchItemsText`, `SearchItems`, `SearchResponse`.

- [ ] **Step 3: Add storage functions** — acrescentar a `internal/storage/search_storage.go` (adicionar `"collection-manager-backend/internal/search"` e `"strings"` aos imports):

```go
// ItemEmbeddings devolve os vetores dos itens do usuário que já foram indexados.
func ItemEmbeddings(ctx context.Context, userID uint) ([]search.Candidate, error) {
	if itemDB == nil {
		return nil, errors.New("conexão com o banco não inicializada")
	}
	var rows []struct {
		ID        int
		Embedding models.Vector
	}
	err := itemDB.WithContext(ctx).Model(&models.Item{}).
		Select("id, embedding").
		Where("user_id = ? AND embedding IS NOT NULL", userID).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]search.Candidate, 0, len(rows))
	for _, r := range rows {
		out = append(out, search.Candidate{ItemID: r.ID, Vector: r.Embedding})
	}
	return out, nil
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
```

- [ ] **Step 4: Implement `internal/handlers/search_handler.go`**

```go
package handlers

import (
	"context"
	"log"
	"net/http"
	"strings"

	"collection-manager-backend/internal/models"
	"collection-manager-backend/internal/search"
	"collection-manager-backend/internal/storage"

	"github.com/gin-gonic/gin"
)

type queryEmbedder interface {
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
}

var (
	queryEmbed         queryEmbedder
	loadItemEmbeddings = storage.ItemEmbeddings
	loadItemsByIDs     = storage.GetItemsByIDs
	searchItemsText    = storage.SearchItemsText
)

// InitSearch define o cliente de IA que gera o vetor das consultas.
func InitSearch(e queryEmbedder) {
	queryEmbed = e
}

type SearchResult struct {
	Item  models.Item `json:"item"`
	Score *float32    `json:"score"`
}

type SearchResponse struct {
	Mode    string         `json:"mode"`
	Results []SearchResult `json:"results"`
}

func SearchItems(c *gin.Context) {
	// Como no cadastro rápido, a busca usa só os itens do próprio usuário (admin incluído).
	userID, _, ok := actorFromContext(c)
	if !ok {
		return
	}

	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Informe o que deseja buscar"})
		return
	}

	ctx := c.Request.Context()
	if results, ok := semanticSearch(ctx, userID, q); ok {
		c.JSON(http.StatusOK, SearchResponse{Mode: "semantic", Results: results})
		return
	}

	items, err := searchItemsText(ctx, userID, q, search.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar itens"})
		return
	}
	results := make([]SearchResult, 0, len(items))
	for _, item := range items {
		results = append(results, SearchResult{Item: item})
	}
	c.JSON(http.StatusOK, SearchResponse{Mode: "text", Results: results})
}

// semanticSearch devolve ok=false quando a busca semântica não é possível
// (IA indisponível ou nenhum item indexado ainda), para cair na busca textual.
func semanticSearch(ctx context.Context, userID uint, q string) ([]SearchResult, bool) {
	if queryEmbed == nil {
		return nil, false
	}
	candidates, err := loadItemEmbeddings(ctx, userID)
	if err != nil || len(candidates) == 0 {
		return nil, false
	}
	vec, err := queryEmbed.EmbedQuery(ctx, q)
	if err != nil {
		log.Printf("erro na IA (busca): %v", err)
		return nil, false
	}

	ranked := search.Rank(vec, candidates, search.MinScore, search.Limit)
	ids := make([]int, len(ranked))
	scores := make(map[int]float32, len(ranked))
	for i, r := range ranked {
		ids[i] = r.ItemID
		scores[r.ItemID] = r.Score
	}

	items, err := loadItemsByIDs(ctx, userID, ids)
	if err != nil {
		return nil, false
	}
	results := make([]SearchResult, 0, len(items))
	for _, item := range items {
		score := scores[item.ID]
		results = append(results, SearchResult{Item: item, Score: &score})
	}
	return results, true
}
```

- [ ] **Step 5: Route + wiring** — `internal/routes/search_routes.go`:

```go
package routes

import (
	"collection-manager-backend/internal/handlers"
	"collection-manager-backend/internal/middleware"

	"github.com/gin-gonic/gin"
)

func RegisterSearchRoutes(r *gin.Engine) {
	r.GET("/search", middleware.AuthMiddleware(), handlers.SearchItems)
}
```

  Em `cmd/api/main.go`: após `handlers.InitAnalyzer(aiClient)` acrescentar `handlers.InitSearch(aiClient)`; após `routes.RegisterQuickAddRoutes(router)` acrescentar `routes.RegisterSearchRoutes(router)`.

- [ ] **Step 6: Run tests and build**

Run: `go test ./internal/... && go vet ./... && go build ./...`
Expected: PASS.

- [ ] **Step 7: Verify against the real DB** (backend reiniciado, Ollama ligado):

```bash
B=http://localhost:8080
TOKEN=$(curl -s $B/auth/login -H 'Content-Type: application/json' -d '{"email":"admin@example.com","password":"admin123"}' | python -c "import sys,json;print(json.load(sys.stdin)['token'])")
for q in "coisas antigas" "itens de metal" "selos" "100%_x"; do
  echo "== $q"; curl -s -G $B/search --data-urlencode "q=$q" -H "Authorization: Bearer $TOKEN" \
    | python -c "import sys,json;d=json.load(sys.stdin);print(d['mode'],[(r['item']['name'],r['score']) for r in d['results']])"
done
```

Expected: `semantic` com itens coerentes e scores decrescentes; `"100%_x"` sem erro (lista vazia ou poucos itens). Depois, parar o Ollama (`ollama stop gemma4:12b` não basta — fechar o app do Ollama) e repetir "moeda": Expected `text` com itens que contêm "moeda". Reabrir o Ollama.

- [ ] **Step 8: Commit**

```bash
git add internal/storage/search_storage.go internal/handlers/search_handler.go internal/handlers/search_handler_test.go internal/routes/search_routes.go cmd/api/main.go
git commit -m "feat(api): add GET /search with semantic ranking and text fallback

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: `GET /home/summary`

**Files:**
- Modify: `internal/storage/search_storage.go` (acrescentar)
- Create: `internal/handlers/home_handler.go`
- Create: `internal/routes/home_routes.go`
- Modify: `cmd/api/main.go`
- Test: `internal/handlers/home_handler_test.go`

**Interfaces:**
- Consumes: `search.TopTags` (Task 3).
- Produces:
  ```go
  // storage
  type HomeTotals struct { Items int64 `json:"items"`; Collections int64 `json:"collections"`; Categories int64 `json:"categories"`; TotalValue float64 `json:"total_value"` }
  func GetHomeTotals(ctx context.Context, userID uint) (HomeTotals, error)
  func GetRecentItems(ctx context.Context, userID uint, limit int) ([]models.Item, error)
  func GetItemTags(ctx context.Context, userID uint) ([][]string, error)
  // handlers
  type HomeSummaryResponse struct { Totals storage.HomeTotals `json:"totals"`; RecentItems []models.Item `json:"recent_items"`; TopTags []string `json:"top_tags"` }
  func GetHomeSummary(c *gin.Context)
  // routes
  func RegisterHomeRoutes(r *gin.Engine)
  ```

- [ ] **Step 1: Write the failing tests** — `internal/handlers/home_handler_test.go`:

```go
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"collection-manager-backend/internal/models"
	"collection-manager-backend/internal/storage"

	"github.com/gin-gonic/gin"
)

func setupHome(t *testing.T, totalsErr error) (*gin.Engine, *[]uint) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var users []uint

	prevT, prevR, prevG := loadHomeTotals, loadRecentItems, loadItemTags
	loadHomeTotals = func(_ context.Context, uid uint) (storage.HomeTotals, error) {
		users = append(users, uid)
		return storage.HomeTotals{Items: 3, Collections: 2, Categories: 1, TotalValue: 42.5}, totalsErr
	}
	loadRecentItems = func(_ context.Context, uid uint, limit int) ([]models.Item, error) {
		users = append(users, uid)
		if limit != 6 {
			t.Errorf("limit = %d, want 6", limit)
		}
		return []models.Item{{ID: 3}, {ID: 2}}, nil
	}
	loadItemTags = func(_ context.Context, uid uint) ([][]string, error) {
		users = append(users, uid)
		return [][]string{{"moeda", "prata"}, {"moeda"}}, nil
	}
	t.Cleanup(func() { loadHomeTotals, loadRecentItems, loadItemTags = prevT, prevR, prevG })

	r := gin.New()
	r.GET("/home/summary", func(c *gin.Context) {
		c.Set("user_id", uint(1))
		c.Set("user_role", models.AdminRole)
		GetHomeSummary(c)
	})
	return r, &users
}

func TestGetHomeSummary_ReturnsTotalsRecentAndTopTags(t *testing.T) {
	r, users := setupHome(t, nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/home/summary", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	var resp HomeSummaryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Totals != (storage.HomeTotals{Items: 3, Collections: 2, Categories: 1, TotalValue: 42.5}) {
		t.Errorf("totals = %+v", resp.Totals)
	}
	if len(resp.RecentItems) != 2 || resp.RecentItems[0].ID != 3 {
		t.Errorf("recent = %+v", resp.RecentItems)
	}
	if fmt.Sprint(resp.TopTags) != "[moeda prata]" {
		t.Errorf("top tags = %v", resp.TopTags)
	}
	for _, u := range *users {
		if u != 1 {
			t.Errorf("queried user %d, want only 1 (admin's own data)", u)
		}
	}
}

func TestGetHomeSummary_ErrorReturns500(t *testing.T) {
	r, _ := setupHome(t, errors.New("db down"))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/home/summary", nil))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/handlers/ -run TestGetHomeSummary`
Expected: build failure — `undefined: loadHomeTotals`, `storage.HomeTotals`, `GetHomeSummary`.

- [ ] **Step 3: Add storage functions** — acrescentar a `internal/storage/search_storage.go`:

```go
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
```

- [ ] **Step 4: Implement `internal/handlers/home_handler.go`**

```go
package handlers

import (
	"net/http"

	"collection-manager-backend/internal/models"
	"collection-manager-backend/internal/search"
	"collection-manager-backend/internal/storage"

	"github.com/gin-gonic/gin"
)

var (
	loadHomeTotals  = storage.GetHomeTotals
	loadRecentItems = storage.GetRecentItems
	loadItemTags    = storage.GetItemTags
)

type HomeSummaryResponse struct {
	Totals      storage.HomeTotals `json:"totals"`
	RecentItems []models.Item      `json:"recent_items"`
	TopTags     []string           `json:"top_tags"`
}

func GetHomeSummary(c *gin.Context) {
	// Painel pessoal: só os dados do próprio usuário (admin incluído).
	userID, _, ok := actorFromContext(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()

	totals, err := loadHomeTotals(ctx, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao carregar resumo"})
		return
	}
	recent, err := loadRecentItems(ctx, userID, 6)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao carregar resumo"})
		return
	}
	tags, err := loadItemTags(ctx, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao carregar resumo"})
		return
	}
	if recent == nil {
		recent = []models.Item{}
	}

	c.JSON(http.StatusOK, HomeSummaryResponse{
		Totals:      totals,
		RecentItems: recent,
		TopTags:     search.TopTags(tags, 5),
	})
}
```

- [ ] **Step 5: Route + wiring** — `internal/routes/home_routes.go`:

```go
package routes

import (
	"collection-manager-backend/internal/handlers"
	"collection-manager-backend/internal/middleware"

	"github.com/gin-gonic/gin"
)

func RegisterHomeRoutes(r *gin.Engine) {
	r.GET("/home/summary", middleware.AuthMiddleware(), handlers.GetHomeSummary)
}
```

  Em `cmd/api/main.go`, após `routes.RegisterSearchRoutes(router)`: `routes.RegisterHomeRoutes(router)`.

- [ ] **Step 6: Run tests and build**

Run: `go test ./internal/... && go vet ./... && go build ./...`
Expected: PASS.

- [ ] **Step 7: Verify against the real DB**

```bash
curl -s $B/home/summary -H "Authorization: Bearer $TOKEN" | python -c "import sys,json;d=json.load(sys.stdin);print(d['totals'],[i['name'] for i in d['recent_items']],d['top_tags'])"
```

Expected: totais batendo com `select count(*), sum(price) from items where user_id=1`, até 6 itens em ordem decrescente de id, até 5 tags.

- [ ] **Step 8: Commit**

```bash
git add internal/storage/search_storage.go internal/handlers/home_handler.go internal/handlers/home_handler_test.go internal/routes/home_routes.go cmd/api/main.go
git commit -m "feat(api): add GET /home/summary with totals, recent items and top tags

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Frontend — modelos e `HomeService`

**Files:**
- Create: `src/app/models/home.model.ts`
- Create: `src/app/services/home.service.ts`
- Modify: `src/app/models/item.model.ts`

**Interfaces:**
- Consumes: `Item` (`item.model.ts`), `Collection` (`collection.model.ts`), `environment.apiBase`.
- Produces:
  ```ts
  export interface SearchResult { item: Item; score: number | null }
  export interface SearchResponse { mode: 'semantic' | 'text'; results: SearchResult[] }
  export interface HomeTotals { items: number; collections: number; categories: number; total_value: number }
  export interface HomeSummary { totals: HomeTotals; recent_items: Item[]; top_tags: string[] }
  class HomeService { search(q: string): Observable<SearchResponse>; summary(): Observable<HomeSummary> }
  // Item ganha: collection?: Collection | null
  ```

- [ ] **Step 1: Create branch**

```bash
cd C:/Users/joaop/Dev/a3/collection-manager-frontend && git switch develop && git pull --ff-only && git switch -c feature/semantic-search
```

- [ ] **Step 2: `item.model.ts`** — trocar o import para `import { BinaryObject, BinaryObjectPayload, Collection } from './collection.model';` e acrescentar a `Item`, após `binary_object?`:

```ts
  collection?: Collection | null;
```

- [ ] **Step 3: Create `src/app/models/home.model.ts`**

```ts
import { Item } from './item.model';

export interface SearchResult {
  item: Item;
  score: number | null;
}

export interface SearchResponse {
  mode: 'semantic' | 'text';
  results: SearchResult[];
}

export interface HomeTotals {
  items: number;
  collections: number;
  categories: number;
  total_value: number;
}

export interface HomeSummary {
  totals: HomeTotals;
  recent_items: Item[];
  top_tags: string[];
}
```

- [ ] **Step 4: Create `src/app/services/home.service.ts`**

```ts
import { Injectable } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable } from 'rxjs';
import { environment } from '../../environments/environment';
import { HomeSummary, SearchResponse } from '../models/home.model';

@Injectable({ providedIn: 'root' })
export class HomeService {
  private readonly API_BASE = environment.apiBase;

  constructor(private http: HttpClient) {}

  search(q: string): Observable<SearchResponse> {
    const params = new HttpParams().set('q', q);
    return this.http.get<SearchResponse>(`${this.API_BASE}/search`, { params });
  }

  summary(): Observable<HomeSummary> {
    return this.http.get<HomeSummary>(`${this.API_BASE}/home/summary`);
  }
}
```

- [ ] **Step 5: Build** — `npx ng build` → Expected: sucesso (apenas avisos pré-existentes).

- [ ] **Step 6: Commit**

```bash
git add src/app/models/home.model.ts src/app/services/home.service.ts src/app/models/item.model.ts
git commit -m "feat(home): add home models and service

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Frontend — componente `Home`

**Files:**
- Create: `src/app/features/home/home.ts`, `home.html`, `home.scss`
- Test: `src/app/features/home/home.spec.ts`

**Interfaces:**
- Consumes: `HomeService` (Task 7), `AlertService.error`, `Router`, `tagColor` (`shared/utils/tag-color`), `Item`.
- Produces: componente standalone `Home` (selector `app-home`); constante exportada `EXAMPLE_QUERIES`.

- [ ] **Step 1: Write the failing spec** — `src/app/features/home/home.spec.ts`:

```ts
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { of, Subject, throwError } from 'rxjs';
import { Home, EXAMPLE_QUERIES } from './home';
import { HomeService } from '../../services/home.service';
import { AlertService } from '../../services/alert.service';
import { HomeSummary, SearchResponse } from '../../models/home.model';

const summaryData: HomeSummary = {
  totals: { items: 3, collections: 2, categories: 1, total_value: 42.5 },
  recent_items: [{ id: 3, name: 'Selo azul', price: 1, collection_id: 2 }],
  top_tags: ['moeda', 'coisas antigas', 'prata'],
};

const semanticResponse: SearchResponse = {
  mode: 'semantic',
  results: [
    { item: { id: 1, name: 'Moeda de 1 real', price: 0, collection_id: 7 }, score: 0.62 },
    { item: { id: 2, name: 'Moeda de 50 centavos', price: 0, collection_id: 7 }, score: 0.48 },
  ],
};

describe('Home', () => {
  let search: ReturnType<typeof vi.fn>;
  let summary: ReturnType<typeof vi.fn>;
  let navigate: ReturnType<typeof vi.fn>;
  let alertError: ReturnType<typeof vi.fn>;

  function setup(): Home {
    TestBed.configureTestingModule({
      imports: [Home],
      providers: [
        { provide: HomeService, useValue: { search, summary } },
        { provide: Router, useValue: { navigate } },
        { provide: AlertService, useValue: { error: alertError } },
      ],
    });
    const fixture = TestBed.createComponent(Home);
    fixture.detectChanges();
    return fixture.componentInstance;
  }

  beforeEach(() => {
    search = vi.fn();
    summary = vi.fn().mockReturnValue(of(summaryData));
    navigate = vi.fn();
    alertError = vi.fn();
  });

  it('carrega o resumo ao abrir', () => {
    const home = setup();
    expect(summary).toHaveBeenCalled();
    expect(home.summary()).toEqual(summaryData);
  });

  it('sugestões = frases de exemplo + tags mais usadas, sem repetir', () => {
    const home = setup();
    expect(home.suggestions()).toEqual([...EXAMPLE_QUERIES, 'moeda', 'prata']);
  });

  it('busca com Enter e mostra os resultados com o modo', () => {
    search.mockReturnValue(of(semanticResponse));
    const home = setup();
    home.query.set('  coisas antigas ');

    home.onKeydown(new KeyboardEvent('keydown', { key: 'Enter' }));

    expect(search).toHaveBeenCalledWith('coisas antigas');
    expect(home.response()).toEqual(semanticResponse);
    expect(home.lastQuery()).toBe('coisas antigas');
    expect(home.searching()).toBe(false);
  });

  it('fica em "buscando" enquanto espera', () => {
    const pending = new Subject<SearchResponse>();
    search.mockReturnValue(pending);
    const home = setup();
    home.query.set('moeda');

    home.search();
    expect(home.searching()).toBe(true);

    pending.next(semanticResponse);
    pending.complete();
    expect(home.searching()).toBe(false);
  });

  it('não busca com o campo vazio', () => {
    const home = setup();
    home.query.set('   ');
    home.search();
    expect(search).not.toHaveBeenCalled();
  });

  it('clicar numa sugestão preenche o campo e busca', () => {
    search.mockReturnValue(of(semanticResponse));
    const home = setup();

    home.useSuggestion('itens de metal');

    expect(home.query()).toBe('itens de metal');
    expect(search).toHaveBeenCalledWith('itens de metal');
  });

  it('limpar volta às sugestões', () => {
    search.mockReturnValue(of(semanticResponse));
    const home = setup();
    home.useSuggestion('moeda');

    home.clear();

    expect(home.query()).toBe('');
    expect(home.response()).toBeNull();
  });

  it('mostra erro e para de buscar quando a busca falha', () => {
    search.mockReturnValue(throwError(() => new Error('500')));
    const home = setup();
    home.query.set('moeda');

    home.search();

    expect(alertError).toHaveBeenCalled();
    expect(home.searching()).toBe(false);
    expect(home.response()).toBeNull();
  });

  it('continua utilizável quando o resumo falha', () => {
    summary.mockReturnValue(throwError(() => new Error('500')));
    search.mockReturnValue(of(semanticResponse));
    const home = setup();

    expect(home.summary()).toBeNull();
    expect(home.suggestions()).toEqual([...EXAMPLE_QUERIES]);
    home.useSuggestion('moeda');
    expect(home.response()).toEqual(semanticResponse);
  });

  it('clicar num item abre a coleção dele', () => {
    const home = setup();
    home.openItem(semanticResponse.results[0].item);
    expect(navigate).toHaveBeenCalledWith(['/collections', 7, 'items']);
  });

  it('converte score em porcentagem', () => {
    const home = setup();
    expect(home.scorePercent(0.623)).toBe(62);
  });
});
```

- [ ] **Step 2: Run to verify failure**

Run: `npx ng test --watch=false`
Expected: erro de compilação — `Cannot find module './home'`.

- [ ] **Step 3: Implement `src/app/features/home/home.ts`**

```ts
import { Component, OnInit, computed, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { HomeService } from '../../services/home.service';
import { AlertService } from '../../services/alert.service';
import { HomeSummary, SearchResponse } from '../../models/home.model';
import { Item } from '../../models/item.model';
import { tagColor } from '../../shared/utils/tag-color';

/** Frases que mostram a busca por significado funcionando. */
export const EXAMPLE_QUERIES = ['coisas antigas', 'itens de metal', 'presentes e lembranças'];

@Component({
  standalone: true,
  selector: 'app-home',
  imports: [CommonModule, FormsModule],
  templateUrl: './home.html',
  styleUrl: './home.scss',
})
export class Home implements OnInit {
  query = signal('');
  lastQuery = signal('');
  searching = signal(false);
  response = signal<SearchResponse | null>(null);
  summary = signal<HomeSummary | null>(null);

  suggestions = computed(() => {
    const tags = (this.summary()?.top_tags ?? []).filter((t) => !EXAMPLE_QUERIES.includes(t));
    return [...EXAMPLE_QUERIES, ...tags];
  });

  tagStyle = (tag: string) => tagColor(tag);

  constructor(
    private homeService: HomeService,
    private alertService: AlertService,
    private router: Router,
  ) {}

  ngOnInit(): void {
    this.homeService.summary().subscribe({
      next: (s) => this.summary.set(s),
      error: () => this.summary.set(null),
    });
  }

  onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Enter') {
      event.preventDefault();
      this.search();
    }
  }

  search(): void {
    const q = this.query().trim();
    if (!q) return;

    this.searching.set(true);
    this.lastQuery.set(q);
    this.homeService.search(q).subscribe({
      next: (res) => {
        this.response.set(res);
        this.searching.set(false);
      },
      error: () => {
        this.alertService.error('Não foi possível realizar a busca.');
        this.searching.set(false);
      },
    });
  }

  useSuggestion(text: string): void {
    this.query.set(text);
    this.search();
  }

  clear(): void {
    this.query.set('');
    this.response.set(null);
  }

  openItem(item: Item): void {
    this.router.navigate(['/collections', item.collection_id, 'items']);
  }

  scorePercent(score: number): number {
    return Math.round(score * 100);
  }

  imageUrl(item: Item): string | null {
    const bin = item.binary_object;
    if (!bin) return null;
    const ext = bin.extension.toLowerCase();
    const mime = ext === 'jpg' ? 'image/jpeg' : `image/${ext}`;
    return `data:${mime};base64,${bin.base64}`;
  }
}
```

- [ ] **Step 4: Implement `src/app/features/home/home.html`**

```html
<div class="home">
  <section class="hero">
    <h2>O que você procura na sua coleção?</h2>
    <div class="search-row">
      <input
        type="text"
        class="search-input"
        [ngModel]="query()"
        (ngModelChange)="query.set($event)"
        (keydown)="onKeydown($event)"
        [disabled]="searching()"
        placeholder="Ex.: coisas antigas, itens de metal, presentes..."
        aria-label="Buscar na coleção"
      />
      <button class="btn-primary" (click)="search()" [disabled]="searching() || !query().trim()">
        {{ searching() ? 'Buscando...' : 'Buscar' }}
      </button>
    </div>

    @if (!response()) {
      <div class="suggestions">
        @for (s of suggestions(); track s) {
          <button type="button" class="chip" (click)="useSuggestion(s)" [disabled]="searching()">{{ s }}</button>
        }
      </div>
    }
  </section>

  @if (response(); as res) {
    <section class="results">
      <div class="results-header">
        <span class="mode-badge" [class.mode-text]="res.mode === 'text'">
          {{ res.mode === 'semantic' ? '🔎 Busca por significado (IA)' : 'Busca por texto (IA indisponível)' }}
        </span>
        <button type="button" class="btn-secondary" (click)="clear()">Limpar busca</button>
      </div>

      @if (res.results.length === 0) {
        <p class="empty">Nada encontrado para "{{ lastQuery() }}".</p>
      } @else {
        <div class="card-grid">
          @for (r of res.results; track r.item.id) {
            <button type="button" class="item-card" (click)="openItem(r.item)">
              @if (imageUrl(r.item); as src) {
                <img [src]="src" [alt]="r.item.name" />
              } @else {
                <div class="no-image">📦</div>
              }
              <div class="item-info">
                <strong>{{ r.item.name }}</strong>
                @if (r.item.collection) {
                  <span class="meta">{{ r.item.collection.name }} · {{ r.item.collection.category.name }}</span>
                }
                @if (r.item.tags?.length) {
                  <div class="tags">
                    @for (tag of r.item.tags; track tag) {
                      <span class="tag-chip" [style.background]="tagStyle(tag).background" [style.color]="tagStyle(tag).color" [style.borderColor]="tagStyle(tag).border">{{ tag }}</span>
                    }
                  </div>
                }
                @if (r.score !== null) {
                  <div class="relevance" [title]="'Relevância: ' + scorePercent(r.score) + '%'">
                    <div class="bar"><div class="fill" [style.width.%]="scorePercent(r.score)"></div></div>
                    <span>{{ scorePercent(r.score) }}%</span>
                  </div>
                }
              </div>
            </button>
          }
        </div>
      }
    </section>
  }

  @if (summary(); as s) {
    <section class="dashboard">
      <div class="stats">
        <div class="stat"><span class="value">{{ s.totals.items }}</span><span class="label">Itens</span></div>
        <div class="stat"><span class="value">{{ s.totals.collections }}</span><span class="label">Coleções</span></div>
        <div class="stat"><span class="value">{{ s.totals.categories }}</span><span class="label">Categorias</span></div>
        <div class="stat"><span class="value">{{ s.totals.total_value | currency: 'BRL' : 'symbol' : '1.2-2' : 'pt-BR' }}</span><span class="label">Valor total</span></div>
      </div>

      @if (s.recent_items.length > 0) {
        <h3>Últimos itens cadastrados</h3>
        <div class="card-grid small">
          @for (item of s.recent_items; track item.id) {
            <button type="button" class="item-card" (click)="openItem(item)">
              @if (imageUrl(item); as src) {
                <img [src]="src" [alt]="item.name" />
              } @else {
                <div class="no-image">📦</div>
              }
              <div class="item-info">
                <strong>{{ item.name }}</strong>
                @if (item.collection) {
                  <span class="meta">{{ item.collection.name }}</span>
                }
              </div>
            </button>
          }
        </div>
      }
    </section>
  }
</div>
```

- [ ] **Step 5: Implement `src/app/features/home/home.scss`**

```scss
.home {
  max-width: 1100px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 32px;
}

.hero {
  text-align: center;
  padding: 24px 0 8px;

  h2 {
    margin: 0 0 16px;
    font-size: 1.6rem;
    color: var(--text);
  }
}

.search-row {
  display: flex;
  gap: 8px;
  max-width: 680px;
  margin: 0 auto;
}

.search-input {
  flex: 1;
  padding: 14px 16px;
  font-size: 1rem;
  border: 1px solid var(--border-strong);
  border-radius: 10px;
  background: var(--input-bg);
  color: var(--text);

  &:focus {
    outline: none;
    border-color: var(--input-focus);
  }
}

.suggestions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 8px;
  margin-top: 16px;
}

.chip {
  padding: 6px 14px;
  border-radius: 999px;
  border: 1px solid var(--border-strong);
  background: var(--surface-muted);
  color: var(--text-muted);
  font-size: 0.85rem;
  cursor: pointer;

  &:hover:not(:disabled) {
    border-color: var(--input-focus);
    color: var(--text);
  }
}

.results-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}

.mode-badge {
  padding: 4px 12px;
  border-radius: 999px;
  font-size: 0.8rem;
  font-weight: 600;
  background: rgba(99, 102, 241, 0.15);
  color: #6366f1;

  &.mode-text {
    background: var(--surface-muted);
    color: var(--text-muted);
  }
}

.empty {
  text-align: center;
  color: var(--text-muted);
}

.card-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 16px;

  &.small {
    grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
  }
}

.item-card {
  display: flex;
  flex-direction: column;
  text-align: left;
  padding: 0;
  border: 1px solid var(--border);
  border-radius: 10px;
  overflow: hidden;
  background: var(--surface, var(--input-bg));
  color: var(--text);
  cursor: pointer;
  font: inherit;
  transition: transform 0.15s, box-shadow 0.15s;

  &:hover {
    transform: translateY(-2px);
    box-shadow: var(--shadow-sm);
  }

  img,
  .no-image {
    width: 100%;
    aspect-ratio: 4 / 3;
    object-fit: cover;
    background: var(--surface-muted);
  }

  .no-image {
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 2rem;
  }
}

.item-info {
  padding: 10px 12px 12px;
  display: flex;
  flex-direction: column;
  gap: 6px;

  .meta {
    font-size: 0.8rem;
    color: var(--text-muted);
  }
}

.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.tag-chip {
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 0.7rem;
  font-weight: 600;
  border: 1px solid transparent;
}

.relevance {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 0.75rem;
  color: var(--text-muted);

  .bar {
    flex: 1;
    height: 6px;
    border-radius: 3px;
    background: var(--surface-muted);
    overflow: hidden;
  }

  .fill {
    height: 100%;
    background: linear-gradient(90deg, #6366f1, #8b5cf6);
  }
}

.dashboard h3 {
  margin: 24px 0 12px;
  font-size: 1.1rem;
  color: var(--text);
}

.stats {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
}

.stat {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--surface, var(--input-bg));

  .value {
    font-size: 1.4rem;
    font-weight: 700;
    color: var(--text);
  }

  .label {
    font-size: 0.8rem;
    color: var(--text-muted);
  }
}

@media (max-width: 640px) {
  .search-row {
    flex-direction: column;
  }

  .stats {
    grid-template-columns: repeat(2, 1fr);
  }
}
```

- [ ] **Step 6: Run tests and build**

Run: `npx ng test --watch=false && npx ng build`
Expected: os 11 testes novos de `Home` passam; apenas os 2 de `app.spec.ts` falham (pré-existentes); build ok.

- [ ] **Step 7: Commit**

```bash
git add src/app/features/home
git commit -m "feat(home): add home screen with semantic search, suggestions and dashboard

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Frontend — rotas, login e menu

**Files:**
- Modify: `src/app/app.routes.ts`, `src/app/app.html`, `src/app/features/auth/login/login.ts`

**Interfaces:**
- Consumes: `Home` (Task 8).

- [ ] **Step 1: `app.routes.ts`** — importar `import { Home } from './features/home/home';`, acrescentar antes de `categories`:

```ts
  {
    path: 'home',
    component: Home,
    canActivate: [authGuard]
  },
```

  e trocar os redirecionamentos finais para:

```ts
  { path: '', redirectTo: 'home', pathMatch: 'full' },
  { path: '**', redirectTo: 'home' }
```

- [ ] **Step 2: `login.ts`** — trocar `await this.router.navigate(['/categories']);` por `await this.router.navigate(['/home']);`.

- [ ] **Step 3: `app.html`** — dentro de `@if (currentUser()) {` do `<nav>`, antes do link de Categorias:

```html
        <a routerLink="/home" routerLinkActive="active" (click)="closeDrawer()">Início</a>
```

- [ ] **Step 4: Build and test**

Run: `npx ng build && npx ng test --watch=false`
Expected: build ok; testes como na Task 8.

- [ ] **Step 5: Commit**

```bash
git add src/app/app.routes.ts src/app/app.html src/app/features/auth/login/login.ts
git commit -m "feat(home): make home the start page and add it to the menu

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: Verificação E2E, ajuste do `MinScore` e entrega

- [ ] **Step 1:** Com Postgres, Ollama (`gemma4:12b` + `embeddinggemma`), backend e frontend rodando: repetir as buscas da Task 5 Step 7 com os itens reais e avaliar se `MinScore = 0.35` separa bem relevantes de irrelevantes. Se não, ajustar a constante em `internal/search/search.go`, registrar o valor e o motivo no ledger, rodar `go test ./internal/...` e commitar `fix(search): tune MinScore to <valor>`.
- [ ] **Step 2:** Criar um item novo (pelo modal ou cadastro rápido) e confirmar que ele aparece na busca semântica segundos depois (`select embedding is not null from items where id=<novo>`).
- [ ] **Step 3:** `go test ./internal/...` e `npx ng test --watch=false` uma última vez.
- [ ] **Step 4:** `git push -u origin feature/semantic-search` nos dois repositórios e abrir PRs para `develop`, descrições terminando com `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.
