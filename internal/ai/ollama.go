package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrUnavailable indica que o Ollama não respondeu ou retornou erro.
var ErrUnavailable = errors.New("serviço de IA indisponível")

const systemPrompt = `Você é um assistente de um sistema de gerenciamento de coleções.
Dado um item, escreva em português do Brasil:
- "description": uma descrição curta e objetiva do item (2 a 3 frases). Se houver foto, descreva o que é visível (cor, material, estado de conservação). Não invente preço nem informações que não possam ser deduzidas.
- "tags": de 3 a 6 tags curtas, em letras minúsculas, úteis para busca.
Responda somente com o JSON pedido.`

var suggestionSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"description": map[string]any{"type": "string"},
		"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	},
	"required": []string{"description", "tags"},
}

type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

func NewClient(baseURL, model string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		http:    &http.Client{Timeout: 90 * time.Second},
	}
}

type ItemContext struct {
	Name        string
	Collection  string
	Category    string
	ImageBase64 string
}

type ItemSuggestion struct {
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

type chatMessage struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Format   any           `json:"format"`
	Stream   bool          `json:"stream"`
	Think    bool          `json:"think"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
}

func (c *Client) SuggestItemDetails(ctx context.Context, item ItemContext) (ItemSuggestion, error) {
	prompt := fmt.Sprintf("Item: %s\nColeção: %s\nCategoria: %s", item.Name, item.Collection, item.Category)
	user := chatMessage{Role: "user", Content: prompt}
	if item.ImageBase64 != "" {
		user.Images = []string{item.ImageBase64}
	}

	body, err := json.Marshal(chatRequest{
		Model:    c.model,
		Messages: []chatMessage{{Role: "system", Content: systemPrompt}, user},
		Format:   suggestionSchema,
	})
	if err != nil {
		return ItemSuggestion{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return ItemSuggestion{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return ItemSuggestion{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ItemSuggestion{}, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}

	var chat chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chat); err != nil {
		return ItemSuggestion{}, fmt.Errorf("%w: resposta inválida: %v", ErrUnavailable, err)
	}

	var suggestion ItemSuggestion
	if err := json.Unmarshal([]byte(chat.Message.Content), &suggestion); err != nil {
		return ItemSuggestion{}, fmt.Errorf("%w: JSON do modelo inválido: %v", ErrUnavailable, err)
	}
	return suggestion, nil
}
