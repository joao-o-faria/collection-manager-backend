# Busca semântica e tela Home — Design

**Data:** 2026-10-07
**Repositórios:** `collection-manager-backend` e `collection-manager-frontend`
**Base:** `develop` (já contém sugestão com IA e cadastro rápido)

## Objetivo

Criar uma tela **Home** (página inicial após o login) cuja peça central é uma **busca semântica de itens**: o usuário escreve uma frase ("coisas antigas", "itens de metal") e encontra itens pelo **significado**, não só por palavras iguais. Embeddings são gerados localmente pelo Ollama (`embeddinggemma`) e comparados por **similaridade de cosseno**. A Home também mostra sugestões de busca clicáveis e um painel com totais e últimos itens.

### Critérios de sucesso

- Após o login, o usuário cai na Home (`/home`); o header tem o link "Início".
- Uma busca por frase devolve itens relevantes mesmo sem palavras em comum com nome/descrição/tags, ordenados por relevância, com a relevância visível (%).
- Com o Ollama fora do ar, a busca continua funcionando por texto (ILIKE) e a tela indica o modo usado.
- Itens novos/editados (inclusive via cadastro rápido) ficam buscáveis sem ação manual; itens já existentes são indexados ao iniciar o backend.
- Salvar um item continua instantâneo e funciona mesmo com a IA fora do ar.
- O painel mostra total de itens, coleções, categorias, valor total e os 6 últimos itens.

## Abordagem escolhida

Vetores armazenados numa coluna comum `real[]` do Postgres e similaridade calculada em Go (sem pgvector), com fallback para busca textual. Motivos: nenhuma mudança de infraestrutura (o `postgres:16-alpine` atual não tem pgvector; vale para todo o grupo e para o Render), cálculo testável como função pura, desempenho de sobra para coleções pessoais. Migrar para pgvector no futuro reaproveita os mesmos vetores.

Modelo de embedding: **`embeddinggemma`** (Google, ~600 MB, multilíngue, bom em pt-BR). Pré-requisito por máquina: `ollama pull embeddinggemma`.

## Backend

### Banco

- Migration `000010_add_item_embedding`:
  - up: `ALTER TABLE items ADD COLUMN IF NOT EXISTS embedding REAL[];`
  - down: `ALTER TABLE items DROP COLUMN IF EXISTS embedding;`
- `models.Vector` (`[]float32`) com `Scan`/`Value` para `real[]` no formato literal do Postgres (`{0.1,0.2,...}`); `nil` ↔ `NULL`.
- `models.Item` ganha `Embedding models.Vector \`json:"-" gorm:"type:real[]"\`` — nunca sai no JSON.

### `internal/ai` — embeddings

- `(*Client).EmbedDocument(ctx, text) ([]float32, error)` → envia `"title: none | text: " + text`.
- `(*Client).EmbedQuery(ctx, text) ([]float32, error)` → envia `"task: search result | query: " + text`.
- Ambos chamam `POST {OLLAMA_URL}/api/embed` com `{"model": <OLLAMA_EMBED_MODEL>, "input": <texto>}` e leem `embeddings[0]`. Erros de rede, status ≠ 200 (com a mensagem do Ollama) ou resposta sem vetor → `ErrUnavailable`.
- O modelo de embedding é um campo do `Client` com padrão `"embeddinggemma"`. `NewClient(baseURL, model)` mantém a assinatura; `(*Client).WithEmbedModel(name string) *Client` troca o modelo e devolve o próprio client (usado em `main.go` com `OLLAMA_EMBED_MODEL`).

### `internal/search` — lógica pura

- `ItemText(item models.Item) string`: `"<nome>. <descrição>. Tags: a, b. Coleção: X. Categoria: Y."` (partes vazias omitidas).
- `Cosine(a, b []float32) float32`: similaridade de cosseno; 0 se tamanhos diferentes ou norma zero.
- `Rank(query []float32, candidates []Candidate, minScore float32, limit int) []Scored`: calcula o cosseno para cada candidato (`Candidate{ItemID int; Vector []float32}`), descarta `score < minScore`, ordena decrescente e corta em `limit`.
- Constantes: `MinScore = 0.35`, `Limit = 12`.

### Indexação

- `search.Indexer` com `IndexItem(itemID int)`: carrega o item (com coleção e categoria), gera `EmbedDocument(ItemText(item))` e grava `embedding`. Em erro, apenas registra no log (o item fica sem vetor).
- **Ao salvar:** após `CreateItem`, `UpdateItem` e `CreateQuickAdd` responderem com sucesso, o handler dispara `go indexer.IndexItem(id)` (contexto próprio com timeout de 60 s, desacoplado da requisição).
- **No boot:** `go indexer.IndexMissing()` gera vetores para todos os itens com `embedding IS NULL`, um por vez.
- Storage novo: `storage.ItemsMissingEmbedding(ctx) ([]int, error)`, `storage.SetItemEmbedding(ctx, id, vec) error`, `storage.GetItemForIndex(ctx, id) (models.Item, error)` (com `Collection.Category`).

### `GET /search?q=<frase>`

- Autenticado. Usa **só os dados do próprio usuário** (como o cadastro rápido, admin incluído).
- `q` vazio (após trim) → **400**.
- Modo semântico: `EmbedQuery(q)` → `storage.ItemEmbeddings(ctx, userID)` (`[]Candidate` com vetor não nulo) → `search.Rank(..., MinScore, Limit)` → carrega os itens ranqueados (`storage.GetItemsByIDs(ctx, userID, ids)` com coleção, categoria e foto) mantendo a ordem.
- Fallback: se `EmbedQuery` falhar → `storage.SearchItemsText(ctx, userID, q, Limit)` (ILIKE em `name`, `description`, `tags`), `score` = `null`.
- Resposta **200**:

```json
{
  "mode": "semantic",
  "results": [
    { "item": { ...Item com collection.category e binary_object... }, "score": 0.62 }
  ]
}
```

  (`mode` = `"text"` no fallback, com `score: null`.)

### `GET /home/summary`

- Autenticado, só dados do próprio usuário.
- Resposta **200**:

```json
{
  "totals": { "items": 10, "collections": 3, "categories": 2, "total_value": 152.5 },
  "recent_items": [ ...6 itens mais recentes (maior id), com collection e binary_object... ],
  "top_tags": ["moeda", "brasil", "..."]
}
```

  `top_tags`: as 5 tags mais frequentes nos itens do usuário (contagem em Go a partir de `items.tags`).

### Rotas e wiring

- `internal/routes/search_routes.go` (`GET /search`) e `internal/routes/home_routes.go` (`GET /home/summary`), ambos com `AuthMiddleware`.
- `main.go`: `OLLAMA_EMBED_MODEL` (padrão `embeddinggemma`), cria o `Indexer`, injeta nos handlers, dispara `IndexMissing` em goroutine; `.env.example` e README documentam a variável e o `ollama pull embeddinggemma`.

## Frontend

### Rotas e navegação

- Nova rota `home` → `HomeComponent` (`authGuard`). `''` e `**` redirecionam para `home`. O login navega para `/home`.
- Header: link "Início" antes de "Categorias".

### `features/home/home`

1. **Busca:** título "O que você procura na sua coleção?", campo grande + botão "Buscar". Dispara só com Enter/botão; campo vazio não busca. Durante a busca: "Buscando...". Com resultado, selo do modo: "🔎 Busca por significado (IA)" (`semantic`) ou "Busca por texto (IA indisponível)" (`text`).
2. **Sugestões** (quando não há busca feita): chips com 3 frases fixas — "coisas antigas", "itens de metal", "presentes e lembranças" — seguidas das `top_tags`. Clicar preenche o campo e busca.
3. **Resultados:** grade de cards (foto, nome, "Coleção · Categoria", tags, barra de relevância em % quando `score` não é nulo). Clique → `/collections/:collection_id/items`. Lista vazia → "Nada encontrado para '<q>'". Botão/ação para limpar a busca e voltar às sugestões.
4. **Painel:** 4 blocos de totais (itens, coleções, categorias, valor total em R$) e "Últimos itens cadastrados" (cards menores, mesmo clique).

### Serviço e modelos

- `models/home.model.ts`: `SearchResult { item: Item; score: number | null }`, `SearchResponse { mode: 'semantic' | 'text'; results: SearchResult[] }`, `HomeSummary { totals: {...}; recent_items: Item[]; top_tags: string[] }`. `Item` ganha `collection?: Collection` (já vem do backend).
- `services/home.service.ts`: `search(q)` → `GET /search?q=`, `summary()` → `GET /home/summary`.

## Testes

**Backend (sem banco):**
- `models.Vector`: `Value`/`Scan` ida e volta, `NULL`.
- `ai`: `EmbedDocument`/`EmbedQuery` com Ollama simulado (prefixos corretos, modelo, parse de `embeddings[0]`, `ErrUnavailable` em falha).
- `search`: `ItemText`, `Cosine` (idênticos = 1, ortogonais = 0, tamanhos diferentes/zero = 0), `Rank` (ordem, `minScore`, `limit`).
- Handler `/search`: 400 sem `q`; modo semântico com fakes; fallback para texto quando o embed falha; escopo do próprio usuário (admin incluso).
- Handler `/home/summary`: monta totais/top tags a partir de fakes.

**Backend (com banco, manual/E2E):** migration aplicada; `IndexMissing` preenche os itens existentes; salvar item gera vetor; `/search` com Ollama ligado e desligado; `/home/summary` com os dados reais.

**Frontend:** spec do `HomeComponent` — carrega o resumo; busca com Enter mostra resultados; chip preenche e busca; selo do modo; estado sem resultados; não busca vazio; clique no card navega para a coleção.

**E2E:** pelo navegador, buscas como "coisas antigas" e "itens de metal" com os itens reais; ajustar `MinScore` se necessário (registrar o valor final).

## Fora do escopo

- pgvector / índices vetoriais.
- Reindexar itens quando coleção/categoria é renomeada (o texto antigo vale até o item ser editado).
- Buscar coleções/categorias; filtros; paginação; busca a cada tecla.
- Busca por imagem.

## Ajustes feitos na implementação (2026-10-07)

Medidos com `embeddinggemma` nos itens reais do projeto:

- **Corte de relevância:** `MinScore` ficou em **0,24** (consultas sem relação com a coleção chegam no máximo a ~0,21; as relevantes começam em ~0,26), e `Rank` ganhou o corte relativo **`RelativeToTop = 0,85`**: só ficam resultados com nota ≥ 85% da melhor. Ex.: "videogame" → os 3 consoles (0,28–0,30) e não o Hot Wheels (0,16).
- **Busca híbrida:** embeddings pontuam mal palavras soltas ("luvas" × "Luvas de Boxe" = 0,15). Por isso a busca textual (ILIKE) roda **sempre**: itens que contêm a frase literalmente vêm primeiro (com a própria nota de cosseno, se indexados), seguidos dos resultados semânticos, sem repetir, até `Limit`. O `mode` continua `"semantic"`; só vira `"text"` quando a IA está indisponível ou nenhum item foi indexado.
