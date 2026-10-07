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
