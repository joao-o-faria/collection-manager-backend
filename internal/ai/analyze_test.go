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
