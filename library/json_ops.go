package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/akennis/dagor"
	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/operator"
)

// ErrRequiredPathMissing is returned by JSONExtractOp when required=true and the path cannot be traversed.
var ErrRequiredPathMissing = errors.New("required path missing")

const JSONExtractOpDescription = `JSONExtractOp: extracts a value from a JSON string using a dot-separated path. Numeric path segments index into arrays (e.g. "meals.0.name"). Inputs: JSON *string, Path *string. Output: Value string (JSON-encoded leaf, or "" if not found).`

type JSONExtractOp struct {
	JSON     *string `dag:"input"`
	Path     *string `dag:"input"`
	Value    string  `dag:"output"`
	required bool    // set via Params("required": true); not a dag field
}

func (op *JSONExtractOp) Setup(p *config.Params) error {
	op.required = p.GetBool("required", false)
	return nil
}
func (op *JSONExtractOp) Reset() error { return nil }
func (op *JSONExtractOp) Run(ctx context.Context) error {
	var root any
	if err := json.Unmarshal([]byte(*op.JSON), &root); err != nil {
		snippet := *op.JSON
		if len(snippet) > 50 {
			snippet = snippet[:50] + "..."
		}
		return fmt.Errorf("JSONExtractOp: invalid JSON (starts with %q): %w", snippet, err)
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
				if op.required {
					return fmt.Errorf("JSONExtractOp: missing key %q in path %q: %w", key, *op.Path, ErrRequiredPathMissing)
				}
				op.Value = ""
				slog.DebugContext(ctx, "JSONExtractOp.missing_key", "run_id", dagor.RunID(ctx), "key", key)
				return nil
			}
			cur = next
		case []any:
			idx, err := strconv.Atoi(key)
			if err != nil || idx < 0 || idx >= len(container) {
				if op.required {
					return fmt.Errorf("JSONExtractOp: index %q out of range in path %q (len %d): %w", key, *op.Path, len(container), ErrRequiredPathMissing)
				}
				op.Value = ""
				slog.DebugContext(ctx, "JSONExtractOp.invalid_index", "run_id", dagor.RunID(ctx), "key", key, "array_len", len(container))
				return nil
			}
			cur = container[idx]
		default:
			if op.required {
				return fmt.Errorf("JSONExtractOp: cannot traverse %T at key %q in path %q: %w", cur, key, *op.Path, ErrRequiredPathMissing)
			}
			op.Value = ""
			slog.DebugContext(ctx, "JSONExtractOp.non_traversable", "run_id", dagor.RunID(ctx), "path", *op.Path, "type", fmt.Sprintf("%T", cur))
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
	return nil
}

func init() {
	operator.RegisterOp[JSONExtractOp]()
}
