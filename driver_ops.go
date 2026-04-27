package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

//go:embed prompts/dag_design.md
var dagDesignPromptTemplate string

//go:embed prompts/dag_design_refine.md
var dagDesignRefinePromptTemplate string

// AINodeDiag contains diagnostics for a single AI-powered node.
type AINodeDiag struct {
	Op        string         `json:"op"`
	Inputs    map[string]any `json:"inputs"`
	Output    any            `json:"output"`
	Reasoning string         `json:"reasoning"`
}

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
	for _, prefix := range []string{"```json", "```go", "```"} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			s = strings.TrimSuffix(strings.TrimSpace(s), "```")
			return strings.TrimSpace(s)
		}
	}
	return s
}

// buildCodegenPrompt assembles the Claude prompt for solution code generation.
// compileContext is empty for the initial attempt; for retries it contains the
// compile error and the previously generated code.
func buildCodegenPrompt(task, libraryDescription, approvedDesign, compileContext string) string {
	var cc string
	if compileContext != "" {
		cc = compileContext + "\n"
	}
	return strings.NewReplacer(
		"{{LIBRARY_DESCRIPTION}}", libraryDescription,
		"{{TASK}}", task,
		"{{APPROVED_DESIGN}}", approvedDesign,
		"{{COMPILE_CONTEXT}}", cc,
	).Replace(codegenPromptTemplate)
}

// DAGDesignOp calls Claude to produce a human-readable DAG design (no code).
type DAGDesignOp struct {
	Prompt             *string `dag:"input"`
	LibraryDescription *string `dag:"input"`
	Design             string  `dag:"output"`
}

func (op *DAGDesignOp) Setup(params *config.Params) error { return nil }
func (op *DAGDesignOp) Reset() error                      { return nil }
func (op *DAGDesignOp) Run(ctx context.Context) error {
	log.Printf("[DEBUG] DAGDesignOp: calling Claude for design")
	apiKey := os.Getenv("CLAUDE_API_KEY")
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	prompt := strings.NewReplacer(
		"{{LIBRARY_DESCRIPTION}}", *op.LibraryDescription,
		"{{TASK}}", *op.Prompt,
	).Replace(dagDesignPromptTemplate)

	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: 4096,
		System: []anthropic.TextBlockParam{
			{Text: "Respond only with a structured DAG design document. No Go code."},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return fmt.Errorf("DAGDesignOp: %w", err)
	}

	for _, block := range msg.Content {
		if block.Type == "text" {
			op.Design += block.Text
		}
	}
	op.Design = strings.TrimSpace(op.Design)
	log.Printf("[DEBUG] DAGDesignOp: received design (%d bytes)", len(op.Design))
	return nil
}

// DAGDesignRefineOp calls Claude to refine an existing DAG design based on feedback.
type DAGDesignRefineOp struct {
	Prompt             *string `dag:"input"`
	LibraryDescription *string `dag:"input"`
	PreviousDesign     *string `dag:"input"`
	Feedback           *string `dag:"input"`
	Design             string  `dag:"output"`
}

func (op *DAGDesignRefineOp) Setup(params *config.Params) error { return nil }
func (op *DAGDesignRefineOp) Reset() error                      { return nil }
func (op *DAGDesignRefineOp) Run(ctx context.Context) error {
	log.Printf("[DEBUG] DAGDesignRefineOp: calling Claude for refinement")
	apiKey := os.Getenv("CLAUDE_API_KEY")
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	prompt := strings.NewReplacer(
		"{{LIBRARY_DESCRIPTION}}", *op.LibraryDescription,
		"{{TASK}}", *op.Prompt,
		"{{PREVIOUS_DESIGN}}", *op.PreviousDesign,
		"{{FEEDBACK}}", *op.Feedback,
	).Replace(dagDesignRefinePromptTemplate)

	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: 4096,
		System: []anthropic.TextBlockParam{
			{Text: "Respond only with a structured DAG design document. No Go code."},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return fmt.Errorf("DAGDesignRefineOp: %w", err)
	}

	for _, block := range msg.Content {
		if block.Type == "text" {
			op.Design += block.Text
		}
	}
	op.Design = strings.TrimSpace(op.Design)
	log.Printf("[DEBUG] DAGDesignRefineOp: received refined design (%d bytes)", len(op.Design))
	return nil
}

// GenerateOp calls Claude to generate Go source for the solution.
type GenerateOp struct {
	Prompt             *string `dag:"input"`
	LibraryDescription *string `dag:"input"`
	ApprovedDesign     *string `dag:"input"`
	GoFiles            string  `dag:"output"` // raw Go source
}

func (op *GenerateOp) Setup(params *config.Params) error { return nil }
func (op *GenerateOp) Reset() error                      { return nil }
func (op *GenerateOp) Run(ctx context.Context) error {
	log.Printf("[DEBUG] GenerateOp: calling Claude")
	apiKey := os.Getenv("CLAUDE_API_KEY")
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	approvedDesign := ""
	if op.ApprovedDesign != nil {
		approvedDesign = *op.ApprovedDesign
	}
	prompt := buildCodegenPrompt(*op.Prompt, *op.LibraryDescription, approvedDesign, "")
	stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: 32000,
		System: []anthropic.TextBlockParam{
			{Text: "Respond with Go source code wrapped in a single ```go ... ``` code fence. No explanation, no other text."},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	msg := anthropic.Message{}
	for stream.Next() {
		msg.Accumulate(stream.Current())
	}
	if err := stream.Err(); err != nil {
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
	ApprovedDesign     *string `dag:"input"`
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

	approvedDesign := ""
	if op.ApprovedDesign != nil {
		approvedDesign = *op.ApprovedDesign
	}
	prompt := buildCodegenPrompt(*op.Prompt, *op.LibraryDescription, approvedDesign, errorContext)
	stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: 32000,
		System: []anthropic.TextBlockParam{
			{Text: "Respond with Go source code wrapped in a single ```go ... ``` code fence. No explanation, no other text."},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	msg := anthropic.Message{}
	for stream.Next() {
		msg.Accumulate(stream.Current())
	}
	if err := stream.Err(); err != nil {
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

// EnvVarSpec describes a single environment variable for the MCPB manifest.
type EnvVarSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Sensitive   bool   `json:"sensitive"`
	Required    bool   `json:"required"`
}

// EnvScanOp scans generated Go source for os.Getenv calls and AI op usage.
type EnvScanOp struct {
	GoFiles         *string `dag:"input"`
	RequiredEnvVars string  `dag:"output"` // JSON array of EnvVarSpec
}

func (op *EnvScanOp) Setup(params *config.Params) error { return nil }
func (op *EnvScanOp) Reset() error                      { return nil }
func (op *EnvScanOp) Run(ctx context.Context) error {
	log.Printf("[DEBUG] EnvScanOp: scanning generated Go source for env vars")
	src := *op.GoFiles

	knownDescriptions := map[string]EnvVarSpec{
		"CLAUDE_API_KEY": {
			Name:        "CLAUDE_API_KEY",
			Description: "Anthropic Claude API key (required for AI ops)",
			Sensitive:   true,
			Required:    false,
		},
	}

	// Collect unique env var names from os.Getenv calls
	re := regexp.MustCompile(`os\.Getenv\("([^"]+)"\)`)
	matches := re.FindAllStringSubmatch(src, -1)
	seen := make(map[string]bool)
	var specs []EnvVarSpec
	for _, m := range matches {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if spec, ok := knownDescriptions[name]; ok {
			specs = append(specs, spec)
		} else {
			specs = append(specs, EnvVarSpec{Name: name, Description: name})
		}
	}

	// Check for AI op usage
	aiOpRe := regexp.MustCompile(`\b\w*(AI|Compute)\w*Op\b`)
	hasAI := strings.Contains(src, "AIComputeOp") ||
		strings.Contains(src, "ModeSelectOp") ||
		aiOpRe.MatchString(src)
	if hasAI && !seen["CLAUDE_API_KEY"] {
		specs = append(specs, knownDescriptions["CLAUDE_API_KEY"])
	}

	b, err := json.Marshal(specs)
	if err != nil {
		return fmt.Errorf("EnvScanOp marshal: %w", err)
	}
	op.RequiredEnvVars = string(b)
	log.Printf("[DEBUG] EnvScanOp: found %d env vars", len(specs))
	return nil
}

// MCPBManifestPromptOp interactively prompts the user for MCPB manifest fields.
type MCPBManifestPromptOp struct {
	Prompt          *string `dag:"input"`
	BinPath         *string `dag:"input"`
	RequiredEnvVars *string `dag:"input"`
	Name            string  `dag:"output"`
	DisplayName     string  `dag:"output"`
	Description     string  `dag:"output"`
	Author          string  `dag:"output"`
}

func (op *MCPBManifestPromptOp) Setup(params *config.Params) error { return nil }
func (op *MCPBManifestPromptOp) Reset() error                      { return nil }
func (op *MCPBManifestPromptOp) Run(ctx context.Context) error {
	if *op.BinPath == "COMPILE_FAILED" || *op.BinPath == "" {
		log.Printf("[DEBUG] MCPBManifestPromptOp: skipped (compile failed or no bin)")
		return nil
	}

	prompt := *op.Prompt

	// Compute defaultName: lowercase slug of first 40 chars
	slug := strings.ToLower(prompt)
	if len(slug) > 40 {
		slug = slug[:40]
	}
	var slugBuilder strings.Builder
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			slugBuilder.WriteRune(r)
		} else {
			slugBuilder.WriteRune('-')
		}
	}
	defaultName := strings.Trim(slugBuilder.String(), "-")
	for strings.Contains(defaultName, "--") {
		defaultName = strings.ReplaceAll(defaultName, "--", "-")
	}

	// Compute defaultDisplay: title-case words from defaultName
	words := strings.Split(defaultName, "-")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	defaultDisplay := strings.Join(words, " ")

	// Compute defaultDesc: first line of prompt, truncated to 120 chars
	firstLine := prompt
	if idx := strings.IndexAny(prompt, "\n\r"); idx >= 0 {
		firstLine = prompt[:idx]
	}
	if len(firstLine) > 120 {
		firstLine = firstLine[:120]
	}

	// Print env vars summary
	if op.RequiredEnvVars != nil && *op.RequiredEnvVars != "" {
		var envSpecs []EnvVarSpec
		if err := json.Unmarshal([]byte(*op.RequiredEnvVars), &envSpecs); err == nil && len(envSpecs) > 0 {
			fmt.Println("\n--- Required environment variables for manifest ---")
			for _, s := range envSpecs {
				fmt.Printf("  %s: %s\n", s.Name, s.Description)
			}
		}
	}

	reader := bufio.NewReader(os.Stdin)
	readField := func(label, def string) string {
		fmt.Printf("%s [%s]: ", label, def)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return def
		}
		return line
	}

	fmt.Println("\n--- MCPB Manifest ---")
	op.Name = readField("Name", defaultName)
	op.DisplayName = readField("Display name", defaultDisplay)
	op.Description = readField("Description", firstLine)
	op.Author = readField("Author", "")

	log.Printf("[DEBUG] MCPBManifestPromptOp: name=%q display=%q author=%q", op.Name, op.DisplayName, op.Author)
	return nil
}

// PackageMCPBOp creates a .mcpb ZIP package from the compiled solution binary and a generated manifest.
type PackageMCPBOp struct {
	BinPath         *string `dag:"input"`
	Name            *string `dag:"input"`
	DisplayName     *string `dag:"input"`
	Description     *string `dag:"input"`
	Author          *string `dag:"input"`
	RequiredEnvVars *string `dag:"input"` // JSON []EnvVarSpec
	MCPBPath        string  `dag:"output"`
}

func (op *PackageMCPBOp) Setup(params *config.Params) error { return nil }
func (op *PackageMCPBOp) Reset() error                      { return nil }
func (op *PackageMCPBOp) Run(ctx context.Context) error {
	if *op.BinPath == "COMPILE_FAILED" || *op.BinPath == "" {
		log.Printf("[DEBUG] PackageMCPBOp: skipped (compile failed or no bin)")
		return nil
	}

	var specs []EnvVarSpec
	if op.RequiredEnvVars != nil && *op.RequiredEnvVars != "" {
		if err := json.Unmarshal([]byte(*op.RequiredEnvVars), &specs); err != nil {
			log.Printf("[DEBUG] PackageMCPBOp: failed to parse RequiredEnvVars: %v", err)
		}
	}

	type userConfigEntry struct {
		Type        string `json:"type"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Sensitive   bool   `json:"sensitive"`
		Required    bool   `json:"required"`
	}
	type mcpConfig struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
	}
	type serverBlock struct {
		Type       string    `json:"type"`
		EntryPoint string    `json:"entry_point"`
		MCPConfig  mcpConfig `json:"mcp_config"`
	}
	type compatibility struct {
		Platforms []string `json:"platforms"`
	}
	type authorBlock struct {
		Name string `json:"name"`
	}
	type manifestDoc struct {
		ManifestVersion string                     `json:"manifest_version"`
		Name            string                     `json:"name"`
		DisplayName     string                     `json:"display_name"`
		Author          authorBlock                `json:"author"`
		Version         string                     `json:"version"`
		Description     string                     `json:"description"`
		Server          serverBlock                `json:"server"`
		ToolsGenerated  bool                       `json:"tools_generated"`
		UserConfig      map[string]userConfigEntry `json:"user_config"`
		Compatibility   compatibility              `json:"compatibility"`
	}

	envMap := make(map[string]string)
	userConfig := make(map[string]userConfigEntry)
	for _, s := range specs {
		lowerName := strings.ToLower(s.Name)
		envMap[s.Name] = fmt.Sprintf("${user_config.%s}", lowerName)
		userConfig[lowerName] = userConfigEntry{
			Type:        "string",
			Title:       s.Description,
			Description: s.Description,
			Sensitive:   s.Sensitive,
			Required:    s.Required,
		}
	}

	m := manifestDoc{
		ManifestVersion: "0.3",
		Name:            *op.Name,
		DisplayName:     *op.DisplayName,
		Author:          authorBlock{Name: *op.Author},
		Version:         "1.0.0",
		Description:     *op.Description,
		Server: serverBlock{
			Type:       "binary",
			EntryPoint: "server/solution_bin.exe",
			MCPConfig: mcpConfig{
				Command: "${__dirname}/server/solution_bin.exe",
				Args:    []string{"--mode", "mcp"},
				Env:     envMap,
			},
		},
		ToolsGenerated: true,
		UserConfig:     userConfig,
		Compatibility:  compatibility{Platforms: []string{"win32"}},
	}

	manifestBytes, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("PackageMCPBOp marshal manifest: %w", err)
	}

	binBytes, err := os.ReadFile(*op.BinPath)
	if err != nil {
		return fmt.Errorf("PackageMCPBOp read bin: %w", err)
	}

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)

	mf, err := zw.Create("manifest.json")
	if err != nil {
		return fmt.Errorf("PackageMCPBOp create manifest.json in zip: %w", err)
	}
	if _, err := mf.Write(manifestBytes); err != nil {
		return fmt.Errorf("PackageMCPBOp write manifest.json: %w", err)
	}

	bf, err := zw.Create("server/solution_bin.exe")
	if err != nil {
		return fmt.Errorf("PackageMCPBOp create bin in zip: %w", err)
	}
	if _, err := bf.Write(binBytes); err != nil {
		return fmt.Errorf("PackageMCPBOp write bin: %w", err)
	}

	if err := zw.Close(); err != nil {
		return fmt.Errorf("PackageMCPBOp close zip: %w", err)
	}

	outPath := filepath.Join(filepath.Dir(filepath.Dir(*op.BinPath)), "solution.mcpb")
	if err := os.WriteFile(outPath, zipBuf.Bytes(), 0644); err != nil {
		return fmt.Errorf("PackageMCPBOp write mcpb: %w", err)
	}

	op.MCPBPath = outPath
	log.Printf("[DEBUG] PackageMCPBOp: written to %s (%d bytes)", outPath, zipBuf.Len())
	return nil
}
