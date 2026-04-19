package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/panjf2000/ants/v2"
	"github.com/wwz16/dagor"
	"github.com/wwz16/dagor/config"
	"github.com/wwz16/dagor/graph"
	"github.com/wwz16/dagor/operator"
)

// SolutionOutput is the JSON structure written to stdout by the solution binary.
type SolutionOutput struct {
	Result  string       `json:"result"`
	AINodes []AINodeDiag `json:"ai_nodes"`
}

// AINodeDiag contains diagnostics for a single AI-powered node.
type AINodeDiag struct {
	Op        string         `json:"op"`
	Inputs    map[string]any `json:"inputs"`
	Output    any            `json:"output"`
	Reasoning string         `json:"reasoning"`
}

// buildDriverDAG constructs the driver DAG using the fluent builder API.
//
// Data flow:
//
//	PromptOp ─────────────────────────────────────────────────────────────────────┐
//	                                                                               ▼
//	LibraryScanOp ──────────────────────────────────────────────────────► GenerateOp
//	                                                                               │
//	                                                                       WriteFilesOp
//	                                                                               │
//	                                                             CompileOp (on_error: continue)
//	                                                                               │
//	                                                             FallbackOp ◄──────┘
//	                                                             (no-op if compile OK;
//	                                                              else re-generates + recompiles;
//	                                                              hard DAG error if both fail)
//	                                                                               │
//	                                                                             RunOp
//	                                                                               │
//	                                                                           OutputOp
func buildDriverDAG(dagAIModulePath string) (*graph.Graph, error) {
	return graph.NewBuilder("driver_dag").
		Vertex("prompt").Op("PromptOp").
		Output("Prompt", "user_prompt").

		Vertex("libscan").Op("LibraryScanOp").
		Output("LibraryDescription", "lib_desc").

		Vertex("generate").Op("GenerateOp").
		Input("Prompt", "user_prompt").
		Input("LibraryDescription", "lib_desc").
		Output("GoFiles", "go_files").

		Vertex("write").Op("WriteFilesOp").
		Params(map[string]string{"dag_ai_module_path": dagAIModulePath}).
		Input("GoFiles", "go_files").
		Output("TempDir", "temp_dir").

		Vertex("validate").Op("ValidateDAGOp").
		Input("GoFiles", "go_files").
		Output("ValidationError", "dag_validation_error").

		Vertex("compile").Op("CompileOp").
		OnError(config.OnErrorContinue).
		Input("TempDir", "temp_dir").
		Output("BinPath", "bin_path").
		Output("ExitCode", "compile_exit").
		Output("Stderr", "compile_stderr").

		Vertex("fallback").Op("FallbackOp").
		Params(map[string]string{"dag_ai_module_path": dagAIModulePath}).
		Input("Prompt", "user_prompt").
		Input("LibraryDescription", "lib_desc").
		Input("CompileExitCode", "compile_exit").
		Input("CompileStderr", "compile_stderr").
		Input("GoFilesOriginal", "go_files").
		Input("InitialBinPath", "bin_path").
		Input("ValidationError", "dag_validation_error").
		Output("BinPath", "final_bin_path").
		Output("Stderr", "final_compile_stderr").

		Vertex("run").Op("RunOp").
		OnError(config.OnErrorContinue).
		Input("BinPath", "final_bin_path").
		Input("CompileStderr", "final_compile_stderr").
		Output("Stdout", "run_stdout").
		Output("Stderr", "run_stderr").
		Output("ExitCode", "run_exit").

		Vertex("output").Op("OutputOp").
		Input("RawStdout", "run_stdout").
		Input("RawStderr", "run_stderr").
		Input("ExitCode", "run_exit").
		Output("Result", "final_result").
		Output("AINodes", "final_ai_nodes").
		Output("ErrorMsg", "run_error").

		Build()
}

func printResults(result string, aiNodesRaw any) {
	fmt.Println("\n--- Result ---")
	fmt.Println(result)

	// aiNodesRaw is *string containing JSON array
	nodesPtr, ok := aiNodesRaw.(*string)
	if !ok || nodesPtr == nil || *nodesPtr == "" || *nodesPtr == "null" {
		return
	}

	var nodes []AINodeDiag
	if err := json.Unmarshal([]byte(*nodesPtr), &nodes); err != nil || len(nodes) == 0 {
		return
	}

	fmt.Println("\n--- AI-Powered Node Diagnostics ---")
	for _, n := range nodes {
		fmt.Println(n.Op)
		var inputParts []string
		for k, v := range n.Inputs {
			inputParts = append(inputParts, fmt.Sprintf("%s=%v", k, v))
		}
		fmt.Printf("  Inputs:    %s\n", strings.Join(inputParts, ", "))
		fmt.Printf("  Output:    %v\n", n.Output)
		fmt.Printf("  Reasoning: %s\n", n.Reasoning)
	}
}

func registerDriverOps() {
	operator.RegisterOp[PromptOp]()
	operator.RegisterOp[LibraryScanOp]()
	operator.RegisterOp[GenerateOp]()
	operator.RegisterOp[WriteFilesOp]()
	operator.RegisterOp[ValidateDAGOp]()
	operator.RegisterOp[CodegenOp]()
	operator.RegisterOp[CompileOp]()
	operator.RegisterOp[FallbackOp]()
	operator.RegisterOp[RunOp]()
	operator.RegisterOp[OutputOp]()
}

func main() {
	registerDriverOps()

	// During `go run .`, use the source dir as the dag-ai module path for the replace directive.
	modulePath, err := filepath.Abs(".")
	if err != nil {
		log.Fatalf("filepath.Abs: %v", err)
	}

	pool, err := ants.NewPool(10)
	if err != nil {
		log.Fatalf("ants.NewPool: %v", err)
	}
	defer pool.Release()

	g, err := buildDriverDAG(modulePath)
	if err != nil {
		log.Fatalf("buildDriverDAG: %v", err)
	}
	eng, err := dagor.NewEngine(g, pool)
	if err != nil {
		log.Fatalf("NewEngine: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	runErr := eng.Run(ctx)

	resultRaw, _ := eng.GetOutput("final_result")
	errMsgRaw, _ := eng.GetOutput("run_error")
	aiNodesRaw, _ := eng.GetOutput("final_ai_nodes")

	result := ""
	if resultRaw != nil {
		result = *(resultRaw.(*string))
	}
	errMsg := ""
	if errMsgRaw != nil {
		errMsg = *(errMsgRaw.(*string))
	}

	cancel()
	eng.Close(ctx)

	if errMsg == "" && result != "" {
		printResults(result, aiNodesRaw)
		return
	}

	// OutputOp never ran (upstream stage failed before it could set errMsg).
	if errMsg == "" {
		if runErr != nil {
			errMsg = fmt.Sprintf("pipeline error: %v", runErr)
		} else {
			errMsg = "pipeline produced no output (check debug logs)"
		}
	}

	fmt.Fprintf(os.Stderr, "Failed: %s\n", errMsg)
	os.Exit(1)
}
