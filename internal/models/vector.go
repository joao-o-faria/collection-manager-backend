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
