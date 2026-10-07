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
