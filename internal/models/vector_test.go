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
