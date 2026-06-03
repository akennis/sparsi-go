package library

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/akennis/dagor/config"
	"github.com/akennis/dagor/operator"
)

const FileReadOpDescription = "FileReadOp: reads a file from disk. Input: Path *string. Output: Content string."
const EnvOpDescription = "EnvOp: reads an environment variable. Input: Name *string. Output: Value string (empty if unset)."
const HTTPGetOpDescription = "HTTPGetOp: performs an HTTP GET request. Input: URL *string. Outputs: Body string, StatusCode int."

type FileReadOp struct {
	Path    *string `dag:"input"`
	Content string  `dag:"output"`
}

func (op *FileReadOp) Setup(_ *config.Params) error { return nil }
func (op *FileReadOp) Reset() error                 { return nil }
func (op *FileReadOp) Run(_ context.Context) error {
	data, err := os.ReadFile(*op.Path)
	if err != nil {
		return fmt.Errorf("FileReadOp: %w", err)
	}
	op.Content = string(data)
	return nil
}

type EnvOp struct {
	Name  *string `dag:"input"`
	Value string  `dag:"output"`
}

func (op *EnvOp) Setup(_ *config.Params) error { return nil }
func (op *EnvOp) Reset() error                 { return nil }
func (op *EnvOp) Run(_ context.Context) error {
	op.Value = os.Getenv(*op.Name)
	return nil
}

type HTTPGetOp struct {
	URL        *string
	Body       string
	StatusCode int
}

func (op *HTTPGetOp) Setup(_ *config.Params) error { return nil }
func (op *HTTPGetOp) Reset() error                 { return nil }
func (op *HTTPGetOp) Run(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, *op.URL, nil)
	if err != nil {
		return fmt.Errorf("HTTPGetOp: build request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTPGetOp: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("HTTPGetOp: read body: %w", err)
	}
	op.Body = string(data)
	op.StatusCode = resp.StatusCode
	return nil
}
func (op *HTTPGetOp) InputFields() map[string]any { return map[string]any{"URL": &op.URL} }
func (op *HTTPGetOp) OutputFields() map[string]any {
	return map[string]any{"Body": &op.Body, "StatusCode": &op.StatusCode}
}
func (op *HTTPGetOp) SetInputField(field string, value any) error {
	if field != "URL" {
		return fmt.Errorf("field %s is not defined", field)
	}
	val, ok := value.(*string)
	if !ok {
		return fmt.Errorf("field URL: expected *string, got %T", value)
	}
	op.URL = val
	return nil
}
func (op *HTTPGetOp) ResetFields() { op.URL = nil; op.Body = ""; op.StatusCode = 0 }

func init() {
	operator.RegisterOp[FileReadOp]()
	operator.RegisterOp[EnvOp]()
	operator.RegisterOp[HTTPGetOp]()
}
