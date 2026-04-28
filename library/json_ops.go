package library

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/wwz16/dagor/config"
	"github.com/wwz16/dagor/operator"
)

const JSONExtractOpDescription = `JSONExtractOp: extracts a value from a JSON string using a dot-separated path. Numeric path segments index into arrays (e.g. "meals.0.name"). Inputs: JSON *string, Path *string. Output: Value string (JSON-encoded leaf, or "" if not found).`

type JSONExtractOp struct {
	JSON  *string `dag:"input"`
	Path  *string `dag:"input"`
	Value string  `dag:"output"`
}

func (op *JSONExtractOp) Setup(_ *config.Params) error { return nil }
func (op *JSONExtractOp) Reset() error                 { return nil }
func (op *JSONExtractOp) Run(_ context.Context) error {
	var root any
	if err := json.Unmarshal([]byte(*op.JSON), &root); err != nil {
		return fmt.Errorf("JSONExtractOp: invalid JSON: %w", err)
	}
	parts := strings.Split(*op.Path, ".")
	cur := root
	for _, key := range parts {
		if key == "" {
			continue
		}
		switch container := cur.(type) {
		case map[string]any:
			next, ok := container[key]
			if !ok {
				op.Value = ""
				log.Printf("[DEBUG] JSONExtractOp: key %q not found", key)
				return nil
			}
			cur = next
		case []any:
			idx, err := strconv.Atoi(key)
			if err != nil || idx < 0 || idx >= len(container) {
				op.Value = ""
				log.Printf("[DEBUG] JSONExtractOp: index %q invalid for array of len %d", key, len(container))
				return nil
			}
			cur = container[idx]
		default:
			op.Value = ""
			log.Printf("[DEBUG] JSONExtractOp: path %q hit non-traversable %T", *op.Path, cur)
			return nil
		}
	}
	switch v := cur.(type) {
	case string:
		op.Value = v
	default:
		b, _ := json.Marshal(v)
		op.Value = string(b)
	}
	log.Printf("[DEBUG] JSONExtractOp: path=%q value=%q", *op.Path, op.Value)
	return nil
}

func init() {
	operator.RegisterOp[JSONExtractOp]()
}
