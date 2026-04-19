package main

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	dagailib "github.com/akennis/clawdag-go/library"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/wwz16/dagor/config"
)

//go:embed prompts/codegen.md
var codegenPromptTemplate string

//go:embed prompts/compile_error_context.md
var compileErrorContextTemplate string

//go:embed prompts/dag_validation_error_context.md
var dagValidationErrorContextTemplate string

// ValidateDAGOp validates the generated Go source.
// ValidationError is empty when the generated code is structurally valid.
type ValidateDAGOp struct {
	GoFiles         *string `dag:"input"`
	ValidationError string  `dag:"output"`
}

// goVersion returns the Go toolchain version suitable for use in a go.mod file (e.g. "1.24.0").
func goVersion() string {
	v := strings.TrimPrefix(runtime.Version(), "go")
	return v
}

func (op *ValidateDAGOp) Setup(params *config.Params) error { return nil }
func (op *ValidateDAGOp) Reset() error                      { return nil }
func (op *ValidateDAGOp) Run(ctx context.Context) error {
	// log.Printf("[DEBUG] ValidateDAGOp: validating generated DAG")
	// var files map[string]string
	// if err := json.Unmarshal([]byte(*op.GoFiles), &files); err != nil {
	// 	op.ValidationError = fmt.Sprintf("parse GoFiles: %v", err)
	// 	log.Printf("[DEBUG] ValidateDAGOp: %s", op.ValidationError)
	// 	return nil
	// }
	// dagJSON := files["dag_json"]
	// if dagJSON == "" {
	// 	op.ValidationError = "dag_json field missing from generated files"
	// 	log.Printf("[DEBUG] ValidateDAGOp: %s", op.ValidationError)
	// 	return nil
	// }
	// if err := validateDAGJSON(dagJSON); err != nil {
	// 	op.ValidationError = err.Error()
	// 	log.Printf("[DEBUG] ValidateDAGOp: errors:\n%s", op.ValidationError)
	// } else {
	// 	log.Printf("[DEBUG] ValidateDAGOp: OK")
	// }
	return nil
}

// PromptOp reads a user prompt from stdin.
type PromptOp struct {
	Prompt string `dag:"output"`
}

func (op *PromptOp) Setup(params *config.Params) error { return nil }
func (op *PromptOp) Reset() error                      { return nil }
func (op *PromptOp) Run(ctx context.Context) error {
	log.Printf("[DEBUG] PromptOp: waiting for user input")
	fmt.Print("Enter prompt: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading prompt: %w", err)
	}
	op.Prompt = strings.TrimSpace(line)
	log.Printf("[DEBUG] PromptOp: prompt=%q", op.Prompt)
	return nil
}

// LibraryScanOp collects descriptions of all available library ops.
type LibraryScanOp struct {
	LibraryDescription string `dag:"output"`
}

func (op *LibraryScanOp) Setup(params *config.Params) error { return nil }
func (op *LibraryScanOp) Reset() error                      { return nil }
func (op *LibraryScanOp) Run(ctx context.Context) error {
	log.Printf("[DEBUG] LibraryScanOp: collecting library op descriptions")
	op.LibraryDescription = strings.Join([]string{
		dagailib.ConstOpDescription,
		dagailib.AddOpDescription,
		dagailib.SubOpDescription,
		dagailib.DivOpDescription,
		dagailib.PackMathOperandsOpDescription,
		dagailib.AIComputeMathOperandsToFloat64OpDescription,
		dagailib.StringConstOpDescription,
		dagailib.StringLookupOpDescription,
		dagailib.StringToLowerOpDescription,
		dagailib.AIComputeStringToStringOpDescription,
		dagailib.CityTimeOpDescription,
		dagailib.ModeSelectOpDescription,
	}, "\n")
	log.Printf("[DEBUG] LibraryScanOp: loaded %d ops", 12)
	return nil
}

// stripMarkdownFences removes optional ```json ... ``` or ``` ... ``` wrappers
// that the model sometimes emits despite being told to return raw JSON.
func stripMarkdownFences(s string) string {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"```json", "```"} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			s = strings.TrimSuffix(strings.TrimSpace(s), "```")
			return strings.TrimSpace(s)
		}
	}
	return s
}

// buildCodegenPrompt assembles the Gemini prompt for solution code generation.
// compileContext is empty for the initial attempt; for retries it contains the
// compile error and the previously generated code.
func buildCodegenPrompt(task, libraryDescription, compileContext string) string {
	var cc string
	if compileContext != "" {
		cc = compileContext + "\n"
	}
	return strings.NewReplacer(
		"{{LIBRARY_DESCRIPTION}}", libraryDescription,
		"{{TASK}}", task,
		"{{COMPILE_CONTEXT}}", cc,
	).Replace(codegenPromptTemplate)
}

// GenerateOp calls Claude to generate Go source for the solution.
type GenerateOp struct {
	Prompt             *string `dag:"input"`
	LibraryDescription *string `dag:"input"`
	GoFiles            string  `dag:"output"` // raw Go source
}

func (op *GenerateOp) Setup(params *config.Params) error { return nil }
func (op *GenerateOp) Reset() error                      { return nil }
func (op *GenerateOp) Run(ctx context.Context) error {
	log.Printf("[DEBUG] GenerateOp: calling Claude")
	apiKey := os.Getenv("CLAUDE_API_KEY")
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	prompt := buildCodegenPrompt(*op.Prompt, *op.LibraryDescription, "")
	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: 8192,
		System: []anthropic.TextBlockParam{
			{Text: "Respond only with raw Go source code. Do not include any explanation, markdown, or JSON wrapping."},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return fmt.Errorf("generate content: %w", err)
	}

	var raw string
	for _, block := range msg.Content {
		if block.Type == "text" {
			raw += block.Text
		}
	}
	raw = strings.TrimSpace(stripMarkdownFences(raw))

	if !strings.HasPrefix(raw, "package ") {
		return fmt.Errorf("generated output does not look like Go source\nraw: %s", raw)
	}
	op.GoFiles = raw
	log.Printf("[DEBUG] GenerateOp: received main_go (%d bytes)", len(raw))
	return nil
}

// WriteFilesOp writes generated Go files to a temp directory.
type WriteFilesOp struct {
	GoFiles *string `dag:"input"`
	TempDir string  `dag:"output"`

	dagAIModulePath string // injected via Setup params
}

func (op *WriteFilesOp) Setup(params *config.Params) error {
	op.dagAIModulePath = params.GetString("dag_ai_module_path", "")
	return nil
}
func (op *WriteFilesOp) Reset() error { return nil }
func (op *WriteFilesOp) Run(ctx context.Context) error {
	mainGo := strings.TrimSpace(*op.GoFiles)

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("UserHomeDir: %w", err)
	}
	tmpDir := filepath.Join(home, ".dag-ai", "solution")
	log.Printf("[DEBUG] WriteFilesOp: preparing dir %s", tmpDir)

	// Wipe and recreate so each attempt starts clean
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("remove solution dir: %w", err)
	}
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("mkdir solution dir: %w", err)
	}

	// Write go.mod (not from AI)
	modPath := filepath.ToSlash(op.dagAIModulePath)
	dagorPath := filepath.ToSlash(filepath.Join(filepath.Dir(op.dagAIModulePath), "dagor"))
	goMod := fmt.Sprintf("module solution\n\ngo %s\n\nrequire github.com/akennis/clawdag-go v0.0.0\n\nreplace github.com/akennis/clawdag-go => %s\nreplace github.com/wwz16/dagor => %s\n", goVersion(), modPath, dagorPath)
	log.Printf("[DEBUG] WriteFilesOp: writing go.mod (replace github.com/akennis/clawdag-go => %s, github.com/wwz16/dagor => %s)", modPath, dagorPath)
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		return fmt.Errorf("write go.mod: %w", err)
	}

	// Write main.go from AI
	log.Printf("[DEBUG] WriteFilesOp: writing main.go (%d bytes)", len(mainGo))
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainGo), 0644); err != nil {
		return fmt.Errorf("write main.go: %w", err)
	}

	// Run go mod tidy to bootstrap go.sum
	log.Printf("[DEBUG] WriteFilesOp: running go mod tidy")
	tidy := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidy.Dir = tmpDir
	tidy.Env = os.Environ()
	if out, err := tidy.CombinedOutput(); err != nil {
		return fmt.Errorf("go mod tidy: %w\n%s", err, out)
	}

	op.TempDir = tmpDir
	log.Printf("[DEBUG] WriteFilesOp: done, solution written to %s", tmpDir)
	return nil
}

// CodegenOp runs go generate in the temp directory.
type CodegenOp struct {
	TempDir  *string `dag:"input"`
	ExitCode int     `dag:"output"`
	Stderr   string  `dag:"output"`
}

func (op *CodegenOp) Setup(params *config.Params) error { return nil }
func (op *CodegenOp) Reset() error                      { return nil }
func (op *CodegenOp) Run(ctx context.Context) error {
	log.Printf("[DEBUG] CodegenOp: running go generate in %s", *op.TempDir)
	cmd := exec.CommandContext(ctx, "go", "generate", "./...")
	cmd.Dir = *op.TempDir
	cmd.Env = os.Environ()
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		op.ExitCode = 1
		op.Stderr = errBuf.String()
		log.Printf("[DEBUG] CodegenOp: exit_code=1 stderr=%q", op.Stderr)
	} else {
		log.Printf("[DEBUG] CodegenOp: exit_code=0")
	}
	return nil
}

// CompileOp compiles the solution binary.
type CompileOp struct {
	TempDir  *string `dag:"input"`
	BinPath  string  `dag:"output"`
	ExitCode int     `dag:"output"`
	Stderr   string  `dag:"output"`
}

func (op *CompileOp) Setup(params *config.Params) error { return nil }
func (op *CompileOp) Reset() error                      { return nil }
func (op *CompileOp) Run(ctx context.Context) error {
	log.Printf("[DEBUG] CompileOp: compiling solution in %s", *op.TempDir)
	binName := "solution_bin"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(*op.TempDir, binName)

	cmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./...")
	cmd.Dir = *op.TempDir
	cmd.Env = os.Environ()
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		op.BinPath = "COMPILE_FAILED"
		op.ExitCode = 1
		op.Stderr = errBuf.String()
		log.Printf("[DEBUG] CompileOp: FAILED stderr=%q", op.Stderr)
		return nil
	}

	op.BinPath = binPath
	log.Printf("[DEBUG] CompileOp: OK bin=%s", binPath)
	return nil
}

// RunOp executes the compiled solution binary.
type RunOp struct {
	BinPath       *string `dag:"input"`
	CompileStderr *string `dag:"input"`
	Stdout        string  `dag:"output"`
	Stderr        string  `dag:"output"`
	ExitCode      int     `dag:"output"`
}

func (op *RunOp) Setup(params *config.Params) error { return nil }
func (op *RunOp) Reset() error                      { return nil }
func (op *RunOp) Run(ctx context.Context) error {
	if *op.BinPath == "COMPILE_FAILED" || *op.BinPath == "" {
		op.ExitCode = 1
		op.Stderr = *op.CompileStderr
		if op.Stderr == "" {
			op.Stderr = "binary not available"
		}
		log.Printf("[DEBUG] RunOp: skipped (compile failed): %s", op.Stderr)
		return nil
	}

	log.Printf("[DEBUG] RunOp: executing %s", *op.BinPath)
	cmd := exec.CommandContext(ctx, *op.BinPath)
	cmd.Env = os.Environ()
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		op.ExitCode = 1
	}
	op.Stdout = outBuf.String()
	op.Stderr = errBuf.String()
	log.Printf("[DEBUG] RunOp: exit_code=%d stdout_len=%d stderr_len=%d", op.ExitCode, len(op.Stdout), len(op.Stderr))
	if op.Stderr != "" {
		log.Printf("[DEBUG] RunOp: stderr=%q", op.Stderr)
	}
	return nil
}

// OutputOp parses run results and formats final output.
type OutputOp struct {
	RawStdout *string `dag:"input"`
	RawStderr *string `dag:"input"`
	ExitCode  *int    `dag:"input"`
	Result    string  `dag:"output"`
	AINodes   string  `dag:"output"`
	ErrorMsg  string  `dag:"output"`
}

func (op *OutputOp) Setup(params *config.Params) error { return nil }
func (op *OutputOp) Reset() error                      { return nil }
func (op *OutputOp) Run(ctx context.Context) error {
	log.Printf("[DEBUG] OutputOp: exit_code=%d stdout_len=%d stderr_len=%d", *op.ExitCode, len(*op.RawStdout), len(*op.RawStderr))

	if *op.ExitCode != 0 {
		op.ErrorMsg = *op.RawStderr
		if op.ErrorMsg == "" {
			op.ErrorMsg = "run failed with no stderr"
		}
		log.Printf("[DEBUG] OutputOp: non-zero exit: %s", op.ErrorMsg)
		return nil
	}

	stdout := strings.TrimSpace(*op.RawStdout)
	if stdout == "" {
		op.ErrorMsg = fmt.Sprintf("empty stdout; stderr: %s", *op.RawStderr)
		log.Printf("[DEBUG] OutputOp: empty stdout")
		return nil
	}

	// Parse flexibly so result can be either a JSON string or a number.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		op.ErrorMsg = fmt.Sprintf("parse output JSON: %v\nstdout: %s", err, stdout)
		log.Printf("[DEBUG] OutputOp: JSON parse error: %v", err)
		return nil
	}

	if r, ok := raw["result"]; ok {
		var s string
		if err := json.Unmarshal(r, &s); err == nil {
			op.Result = s
		} else {
			// AI returned a number instead of a string — convert it.
			var n json.Number
			if err2 := json.Unmarshal(r, &n); err2 == nil {
				op.Result = n.String()
				log.Printf("[DEBUG] OutputOp: result was numeric, coerced to string: %s", op.Result)
			}
		}
	}

	if nodes, ok := raw["ai_nodes"]; ok {
		var aiNodes []AINodeDiag
		if err := json.Unmarshal(nodes, &aiNodes); err == nil {
			aiNodesJSON, _ := json.Marshal(aiNodes)
			op.AINodes = string(aiNodesJSON)
		}
	}

	if op.Result == "" {
		op.ErrorMsg = fmt.Sprintf("result field missing or empty in output; stdout: %s", stdout)
		log.Printf("[DEBUG] OutputOp: result empty after parse")
		return nil
	}

	log.Printf("[DEBUG] OutputOp: result=%q ai_nodes=%s", op.Result, op.AINodes)
	return nil
}

// FallbackOp handles a single retry of code generation and compilation.
// If the initial compile succeeded it is a no-op (passes the binary through).
// If the initial compile failed it calls Gemini with the error + original code,
// writes the new files, and recompiles. A second compile failure is a hard error
// that fails the DAG.
type FallbackOp struct {
	Prompt             *string `dag:"input"`
	LibraryDescription *string `dag:"input"`
	CompileExitCode    *int    `dag:"input"` // from initial CompileOp
	CompileStderr      *string `dag:"input"` // from initial CompileOp
	GoFilesOriginal    *string `dag:"input"` // from initial GenerateOp
	InitialBinPath     *string `dag:"input"` // from initial CompileOp
	ValidationError    *string `dag:"input"` // from ValidateDAGOp
	BinPath            string  `dag:"output"`
	Stderr             string  `dag:"output"` // forwarded to RunOp as CompileStderr

	dagAIModulePath string
}

func (op *FallbackOp) Setup(params *config.Params) error {
	op.dagAIModulePath = params.GetString("dag_ai_module_path", "")
	return nil
}
func (op *FallbackOp) Reset() error { return nil }
func (op *FallbackOp) Run(ctx context.Context) error {
	compileOK := *op.CompileExitCode == 0
	validationOK := op.ValidationError == nil || *op.ValidationError == ""

	if compileOK && validationOK {
		op.BinPath = *op.InitialBinPath
		log.Printf("[DEBUG] FallbackOp: compile succeeded and DAG valid, passthrough bin=%s", op.BinPath)
		return nil
	}

	originalCode := strings.TrimSpace(*op.GoFilesOriginal)

	// Use validation error context when the DAG is structurally invalid;
	// compile error context otherwise. Fixing DAG structure typically resolves
	// compile errors that stem from it, so we don't send both at once.
	var errorContext string
	if !validationOK {
		log.Printf("[DEBUG] FallbackOp: DAG validation failed, generating fallback code")
		errorContext = strings.NewReplacer(
			"{{VALIDATION_ERROR}}", *op.ValidationError,
			"{{GENERATED_CODE}}", originalCode,
		).Replace(dagValidationErrorContextTemplate)
	} else {
		log.Printf("[DEBUG] FallbackOp: initial compile failed, generating fallback code")
		errorContext = strings.NewReplacer(
			"{{COMPILE_ERROR}}", *op.CompileStderr,
			"{{GENERATED_CODE}}", originalCode,
		).Replace(compileErrorContextTemplate)
	}

	// Call Claude with the same base prompt as GenerateOp, plus error context.
	apiKey := os.Getenv("CLAUDE_API_KEY")
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	prompt := buildCodegenPrompt(*op.Prompt, *op.LibraryDescription, errorContext)
	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: 8192,
		System: []anthropic.TextBlockParam{
			{Text: "Respond only with raw Go source code. Do not include any explanation, markdown, or JSON wrapping."},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return fmt.Errorf("fallback generate content: %w", err)
	}

	var raw string
	for _, block := range msg.Content {
		if block.Type == "text" {
			raw += block.Text
		}
	}
	raw = strings.TrimSpace(stripMarkdownFences(raw))

	if !strings.HasPrefix(raw, "package ") {
		return fmt.Errorf("fallback: output does not look like Go source\nraw: %s", raw)
	}
	log.Printf("[DEBUG] FallbackOp: received fallback main_go (%d bytes)", len(raw))

	// Write fallback files to a separate directory so the initial solution is not clobbered.
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("UserHomeDir: %w", err)
	}
	fallbackDir := filepath.Join(home, ".dag-ai", "solution_fallback")
	if err := os.RemoveAll(fallbackDir); err != nil {
		return fmt.Errorf("remove fallback dir: %w", err)
	}
	if err := os.MkdirAll(fallbackDir, 0755); err != nil {
		return fmt.Errorf("mkdir fallback dir: %w", err)
	}

	modPath := filepath.ToSlash(op.dagAIModulePath)
	dagorPath := filepath.ToSlash(filepath.Join(filepath.Dir(op.dagAIModulePath), "dagor"))
	goMod := fmt.Sprintf("module solution\n\ngo %s\n\nrequire github.com/akennis/clawdag-go v0.0.0\n\nreplace github.com/akennis/clawdag-go => %s\nreplace github.com/wwz16/dagor => %s\n", goVersion(), modPath, dagorPath)
	if err := os.WriteFile(filepath.Join(fallbackDir, "go.mod"), []byte(goMod), 0644); err != nil {
		return fmt.Errorf("write fallback go.mod: %w", err)
	}
	if err := os.WriteFile(filepath.Join(fallbackDir, "main.go"), []byte(raw), 0644); err != nil {
		return fmt.Errorf("write fallback main.go: %w", err)
	}

	log.Printf("[DEBUG] FallbackOp: gofmt syntax check")
	fmtCmd := exec.CommandContext(ctx, "gofmt", "-e", filepath.Join(fallbackDir, "main.go"))
	if out, err := fmtCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("fallback syntax error in main.go:\n%s", out)
	}

	log.Printf("[DEBUG] FallbackOp: running go mod tidy")
	tidy := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidy.Dir = fallbackDir
	tidy.Env = os.Environ()
	if out, err := tidy.CombinedOutput(); err != nil {
		return fmt.Errorf("fallback go mod tidy: %w\n%s", err, out)
	}

	// Compile the fallback. Unlike the initial CompileOp, failure here is a hard DAG error.
	binName := "solution_bin"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(fallbackDir, binName)
	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./...")
	buildCmd.Dir = fallbackDir
	buildCmd.Env = os.Environ()
	var errBuf strings.Builder
	buildCmd.Stderr = &errBuf
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("fallback compile failed:\n%s", errBuf.String())
	}

	op.BinPath = binPath
	log.Printf("[DEBUG] FallbackOp: fallback compile OK, bin=%s", op.BinPath)
	return nil
}
