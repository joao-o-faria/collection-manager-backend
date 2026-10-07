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

type capturedRequest struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Think    bool   `json:"think"`
	Format   any    `json:"format"`
	Messages []struct {
		Role    string   `json:"role"`
		Content string   `json:"content"`
		Images  []string `json:"images"`
	} `json:"messages"`
}

func newFakeOllama(t *testing.T, reply string, got *capturedRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %q, want /api/chat", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		resp := map[string]any{"message": map[string]string{"role": "assistant", "content": reply}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestSuggestItemDetails_ParsesModelReply(t *testing.T) {
	var got capturedRequest
	srv := newFakeOllama(t, `{"description":"Moeda comemorativa.","tags":["moeda","1998"]}`, &got)
	defer srv.Close()

	client := NewClient(srv.URL, "gemma4:12b")
	s, err := client.SuggestItemDetails(context.Background(), ItemContext{
		Name: "Moeda 1 real", Collection: "Moedas", Category: "Numismática",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Description != "Moeda comemorativa." {
		t.Errorf("description = %q", s.Description)
	}
	if len(s.Tags) != 2 || s.Tags[0] != "moeda" || s.Tags[1] != "1998" {
		t.Errorf("tags = %v", s.Tags)
	}
}

func TestSuggestItemDetails_SendsModelPromptAndSchema(t *testing.T) {
	var got capturedRequest
	srv := newFakeOllama(t, `{"description":"x","tags":[]}`, &got)
	defer srv.Close()

	client := NewClient(srv.URL, "gemma4:12b")
	_, err := client.SuggestItemDetails(context.Background(), ItemContext{
		Name: "Moeda 1 real", Collection: "Moedas", Category: "Numismática",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Model != "gemma4:12b" {
		t.Errorf("model = %q", got.Model)
	}
	if got.Stream || got.Think {
		t.Errorf("stream=%v think=%v, want both false", got.Stream, got.Think)
	}
	if got.Format == nil {
		t.Error("format (JSON schema) not sent")
	}
	if len(got.Messages) == 0 {
		t.Fatal("no messages sent")
	}
	user := got.Messages[len(got.Messages)-1]
	for _, want := range []string{"Moeda 1 real", "Moedas", "Numismática"} {
		if !strings.Contains(user.Content, want) {
			t.Errorf("prompt missing %q: %s", want, user.Content)
		}
	}
	if len(user.Images) != 0 {
		t.Errorf("images = %v, want none", user.Images)
	}
}

func TestSuggestItemDetails_SendsImageWhenPresent(t *testing.T) {
	var got capturedRequest
	srv := newFakeOllama(t, `{"description":"x","tags":[]}`, &got)
	defer srv.Close()

	client := NewClient(srv.URL, "gemma4:12b")
	_, err := client.SuggestItemDetails(context.Background(), ItemContext{
		Name: "Moeda", ImageBase64: "aGVsbG8=",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	user := got.Messages[len(got.Messages)-1]
	if len(user.Images) != 1 || user.Images[0] != "aGVsbG8=" {
		t.Errorf("images = %v, want [aGVsbG8=]", user.Images)
	}
}

func TestSuggestItemDetails_ReturnsErrUnavailableWhenOllamaDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	client := NewClient(url, "gemma4:12b")
	_, err := client.SuggestItemDetails(context.Background(), ItemContext{Name: "Moeda"})
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

func TestSuggestItemDetails_ReturnsErrUnavailableOnServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "gemma4:12b")
	_, err := client.SuggestItemDetails(context.Background(), ItemContext{Name: "Moeda"})
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

func TestSuggestItemDetails_ErrorIncludesOllamaMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"Failed to load image or audio file"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "gemma4:12b").SuggestItemDetails(context.Background(), ItemContext{Name: "Moeda"})
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "Failed to load image") {
		t.Errorf("err = %v, want ErrUnavailable with Ollama's message", err)
	}
}
