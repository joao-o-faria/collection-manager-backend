# Collection Manager Backend

Este é o serviço de backend para o sistema de gerenciamento de coleções, desenvolvido em Go utilizando o framework Gin e GORM para persistência de dados no PostgreSQL.

## 🚀 Como começar

### Pré-requisitos

- [Go](https://go.dev/doc/install) (versão 1.25 ou superior)
- [Docker](https://www.docker.com/get-started) e Docker Compose
- [golang-migrate](https://github.com/golang-migrate/migrate/tree/master/cmd/migrate) (para gerenciar migrações)

### Configuração

1. Clone o repositório.
2. Crie um arquivo `.env` na raiz do projeto (ou edite o existente) com a string de conexão do banco de dados:

```env
DATABASE_URL=postgres://user:pwd@localhost:5433/collection_manager?sslmode=disable
```

### Execução

Para iniciar o servidor:

```bash
go run cmd/api/main.go
```

O servidor estará disponível em `http://localhost:8080`.

## 🗄️ Banco de Dados e Migrações

Utilizamos o `golang-migrate` para gerenciar as alterações no esquema do banco de dados. As migrações estão localizadas no diretório `migrations/`.

### Gerando uma nova migração

Para criar um novo par de arquivos de migração (up e down), execute:

```bash
migrate create -ext sql -dir migrations -seq nome_da_migracao
```

Isso gerará dois arquivos:
- `XXXXXX_nome_da_migracao.up.sql`: Para aplicar as mudanças.
- `XXXXXX_nome_da_migracao.down.sql`: Para reverter as mudanças.

### Executando as migrações

Para aplicar todas as migrações pendentes ("up"):

```bash
migrate -source file://migrations -database "postgres://user:pwd@localhost:5433/collection_manager?sslmode=disable" up
```

Para reverter a última migração aplicada ("down"):

```bash
migrate -source file://migrations -database "postgres://user:pwd@localhost:5433/collection_manager?sslmode=disable" down 1
```

> **Dica:** Você pode substituir a string de conexão pela variável de ambiente configurada no seu terminal se preferir.

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
