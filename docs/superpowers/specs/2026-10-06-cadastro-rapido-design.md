# Cadastro rápido por imagem — Design

**Data:** 2026-10-06
**Repositórios:** `collection-manager-backend` e `collection-manager-frontend`
**Depende de:** `feature/ai-suggest` (cliente Ollama em `internal/ai`)

## Objetivo

Permitir que o usuário cadastre um item a partir de **uma foto**, sem precisar navegar até a categoria e a coleção certas. A IA local (Ollama, `gemma4:12b`) analisa a imagem, decide se o item se encaixa numa categoria/coleção **existente** do usuário ou sugere criar novas, e preenche nome, descrição e tags. O usuário **revisa e ajusta** antes de salvar.

### Critérios de sucesso

- Botão "✨ Cadastro rápido" no header, visível apenas para usuário logado.
- Com uma foto, o usuário chega a uma tela de revisão pré-preenchida (nome, descrição, tags, categoria, coleção).
- A IA reaproveita categorias/coleções existentes quando fazem sentido, inclusive sem palavras em comum (ex.: "Hot Wheels" → "Miniaturas").
- Ao confirmar, categoria/coleção novas e o item são criados **atomicamente**; nenhuma entidade órfã em caso de falha.
- Após salvar, o usuário é levado à página da coleção do item.
- Com o Ollama fora do ar, o fluxo exibe erro e o restante do sistema segue funcionando.

## Abordagem escolhida

O backend envia ao modelo a foto **e** a lista de categorias e coleções do usuário (com ids). O modelo escolhe ids existentes ou sugere nomes novos, num JSON validado por schema. O backend valida a resposta antes de devolvê-la. (Alternativas descartadas: comparação de nomes no código — não casa sinônimos; embeddings — escopo grande demais para esta etapa.)

## Backend

### `internal/ai` — novo método

`(*Client).AnalyzeItemImage(ctx, imageBase64 string, categories []CategoryOption, collections []CollectionOption) (ImageAnalysis, error)`

- `CategoryOption{ID int, Name string}`; `CollectionOption{ID int, Name string, CategoryID int}`.
- Chama `/api/chat` com `stream:false`, `think:false`, a foto em `images` e um JSON schema em `format`. O prompt (pt-BR) lista as opções no formato `id: nome` (coleções com a categoria a que pertencem) e instrui a preferir uma existente quando fizer sentido.
- Resposta do modelo (`ImageAnalysis`):

```json
{
  "name": "string",
  "description": "string",
  "tags": ["string"],
  "category":   { "id": 0, "new_name": "string" },
  "collection": { "id": 0, "new_name": "string" }
}
```

  `id > 0` significa existente; `id == 0` significa nova com `new_name`.
- Erros de rede, status ≠ 200 ou JSON inválido → `ErrUnavailable` (mesmo padrão de `SuggestItemDetails`).

### `POST /quick-add/analyze`

- Autenticado (`AuthMiddleware`). Corpo: `{ "image_base64": "..." }`. Imagem vazia → **400**.
- Busca categorias (`storage.GetCategories`) e coleções (`storage.GetCollections`) visíveis ao usuário (mesmo `scopeByUser` do resto do sistema: admin vê todas), chama `AnalyzeItemImage`.
- **Validação** da resposta do modelo (função pura `resolveAnalysis`, testável sem banco):
  1. Categoria com id que não está na lista do usuário → vira nova (`id=0`) com o `new_name` sugerido; se `new_name` vier vazio, usa `"Geral"`.
  2. Coleção com id que não está na lista do usuário → vira nova com o `new_name` sugerido; se vazio, usa o nome da categoria.
  3. Coleção existente cuja categoria difere da categoria escolhida → a categoria passa a ser a da coleção (a coleção existente manda).
  4. Categoria nova → coleção obrigatoriamente nova (se o modelo escolheu uma coleção existente, ela é descartada e o nome dela não é reaproveitado; usa-se `new_name` ou, se vazio, o nome da categoria).
  5. Nome do item vazio → `"Item sem nome"`. Tags normalizadas com `normalizeTags`.
- Resposta **200**:

```json
{
  "name": "...", "description": "...", "tags": ["..."],
  "category":   { "id": 3, "name": "Numismática", "is_new": false },
  "collection": { "id": 0, "name": "Moedas brasileiras", "is_new": true }
}
```

- IA indisponível → **503** `"Serviço de IA indisponível"`.

### `POST /quick-add`

- Autenticado. Corpo:

```json
{
  "category":   { "id": 3 }          ou { "new_name": "Numismática" },
  "collection": { "id": 7 }          ou { "new_name": "Moedas brasileiras" },
  "item": {
    "name": "...", "description": "...", "tags": ["..."], "price": 0,
    "binary_object": { "base64": "...", "filename": "...", "extension": "..." }
  }
}
```

- Validações (handler): exatamente um entre `id` e `new_name` em cada referência; `new_name`/`item.name` não vazios após trim; `price >= 0`; categoria nova exige coleção nova → **400** caso contrário.
- Nova função `storage.QuickAdd(ctx, userID, isAdmin, input)` executa **em uma transação** (`db.Transaction`):
  1. Categoria: se `id`, verifica que pertence ao usuário (escopo `scopeByUser`) → senão `ErrNotFound`; se `new_name`, cria com `user_id` do usuário.
  2. Coleção: se `id`, verifica escopo e que `collection.category_id == categoria` → senão `ErrNotFound` / `ErrCategoryMismatch`; se `new_name`, cria na categoria resolvida.
  3. Item: cria com a foto (`binary_objects`), descrição e tags normalizadas.
- Respostas: **201** com o item criado (inclui `collection_id`); **404** categoria/coleção inacessível; **400** coleção de outra categoria; **500** demais erros.

### Rotas e wiring

- `internal/routes/quick_add_routes.go` com o grupo `/quick-add` + `AuthMiddleware`; registrado em `main.go`.
- O handler de análise usa o mesmo padrão injetável de `suggest_handler.go` (variáveis de pacote para o analisador e para as buscas de categorias/coleções), permitindo testes sem banco. O cliente `ai.Client` já criado em `main.go` é reutilizado.

## Frontend

### Header (`app.html`)

- Botão "✨ Cadastro rápido" em `.header-actions`, antes do botão de tema, só com `currentUser()`. Em telas ≤ 480px mostra apenas "✨" (com `aria-label`).
- `App` ganha o signal `quickAddOpen` e renderiza `<app-quick-add-modal>` quando aberto.

### `features/quick-add/quick-add-modal`

Três estados (`step = 'upload' | 'analyzing' | 'review'`):

1. **upload** — input de arquivo (`accept="image/*"`) com prévia; botão "Analisar com IA" desabilitado sem foto.
2. **analyzing** — spinner e texto "Analisando imagem... (pode levar alguns segundos)"; fechar fica desabilitado. Em erro: alerta via `AlertService` e volta para **upload** mantendo a foto.
3. **review** — formulário pré-preenchido:
   - **Categoria:** `select` com as categorias do usuário + opção "➕ Nova categoria"; selo "existente"/"nova". Ao escolher "nova", input de nome (pré-preenchido com a sugestão).
   - **Coleção:** `select` com as coleções **da categoria selecionada** + "➕ Nova coleção"; se a categoria for nova, só a opção nova fica disponível. Input de nome quando nova.
   - Nome, descrição, preço (inicia em 0), tags (chips com `tagColor`, adicionar/remover como no modal de item).
   - Botões "Cancelar" e "Cadastrar" (mostra "Cadastrando..." durante a chamada).

Após **201**: fecha o modal e navega para `/collections/:collection_id/items`, onde o item recém-criado aparece (o `AlertService` só tem `error`, então não há alerta de sucesso; a navegação é a confirmação).

### Serviço e modelos

- `models/quick-add.model.ts`: `QuickAddAnalysis`, `ResolvedRef`, `QuickAddRef` (`{id}` | `{new_name}`), `QuickAddRequest`.
- `services/quick-add.service.ts`: `analyze(imageBase64)` → `POST /quick-add/analyze`; `create(req)` → `POST /quick-add`.
- Categorias e coleções para os `select`s vêm de `CategoryService` e `CollectionService` existentes, carregadas ao abrir o modal.

## Testes

**Backend (sem banco):**
- `internal/ai`: `AnalyzeItemImage` com Ollama simulado — envia foto, schema e opções no prompt; faz parse da resposta; `ErrUnavailable` em falhas.
- `resolveAnalysis`: as 5 regras de validação acima.
- Handler `/quick-add/analyze`: 400 sem imagem, 200 com resposta resolvida, 503 com IA fora.
- Handler `/quick-add`: validações de entrada (400) com `storage.QuickAdd` substituído por fake.

**Backend (com banco, manual/E2E):** `QuickAdd` contra o Postgres local — criação de tudo novo, reaproveitamento de existentes, rollback quando a criação do item falha, 404 para id de outro usuário.

**Frontend:** spec do modal com serviços falsos — análise preenche a revisão; trocar categoria filtra coleções; categoria nova força coleção nova; payload `{id}`/`{new_name}` correto; erro na análise volta ao upload; sucesso navega para a coleção.

**E2E:** com backend, frontend, Postgres e Ollama rodando, cadastrar um item por foto pelo navegador.

## Fora do escopo

- Várias fotos/itens de uma vez; captura pela câmera.
- Detecção de item duplicado.
- Estimativa de preço pela IA.
- Imagem para coleção nova (fica vazia; editável depois).
