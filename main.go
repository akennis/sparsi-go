package main

import (
	"bufio"
	"context"
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

// buildDesignDAG constructs the Phase 1 DAG: PromptOp + LibraryScanOp + DAGDesignOp.
func buildDesignDAG() (*graph.Graph, error) {
	return graph.NewBuilder("design_dag").
		Vertex("prompt").Op("PromptOp").
		Output("Prompt", "prompt_out").

		Vertex("libscan").Op("LibraryScanOp").
		Output("LibraryDescription", "library_out").

		Vertex("design").Op("DAGDesignOp").
		Input("Prompt", "prompt_out").
		Input("LibraryDescription", "library_out").
		Output("Design", "design_out").

		Build()
}

// buildRefineDAG constructs the refinement DAG using StringConstOp for the four known values.
func buildRefineDAG(prompt, library, prevDesign, feedback string) (*graph.Graph, error) {
	return graph.NewBuilder("refine_dag").
		Vertex("prompt_const").Op("StringConstOp").
		Params(map[string]string{"Value": prompt}).
		Output("Result", "prompt_const_out").

		Vertex("library_const").Op("StringConstOp").
		Params(map[string]string{"Value": library}).
		Output("Result", "library_const_out").

		Vertex("prev_design_const").Op("StringConstOp").
		Params(map[string]string{"Value": prevDesign}).
		Output("Result", "prev_design_const_out").

		Vertex("feedback_const").Op("StringConstOp").
		Params(map[string]string{"Value": feedback}).
		Output("Result", "feedback_const_out").

		Vertex("refine").Op("DAGDesignRefineOp").
		Input("Prompt", "prompt_const_out").
		Input("LibraryDescription", "library_const_out").
		Input("PreviousDesign", "prev_design_const_out").
		Input("Feedback", "feedback_const_out").
		Output("Design", "design_out").

		Build()
}

// buildCodegenDAG constructs the Phase 3 DAG using StringConstOp for the three known values.
func buildCodegenDAG(prompt, library, approvedDesign, dagAIModulePath string) (*graph.Graph, error) {
	return graph.NewBuilder("codegen_dag").
		Vertex("prompt_const").Op("StringConstOp").
		Params(map[string]string{"Value": prompt}).
		Output("Result", "prompt_const_out").

		Vertex("library_const").Op("StringConstOp").
		Params(map[string]string{"Value": library}).
		Output("Result", "library_const_out").

		Vertex("design_const").Op("StringConstOp").
		Params(map[string]string{"Value": approvedDesign}).
		Output("Result", "design_const_out").

		Vertex("generate").Op("GenerateOp").
		Input("Prompt", "prompt_const_out").
		Input("LibraryDescription", "library_const_out").
		Input("ApprovedDesign", "design_const_out").
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
		Input("Prompt", "prompt_const_out").
		Input("LibraryDescription", "library_const_out").
		Input("ApprovedDesign", "design_const_out").
		Input("CompileExitCode", "compile_exit").
		Input("CompileStderr", "compile_stderr").
		Input("GoFilesOriginal", "go_files").
		Input("InitialBinPath", "bin_path").
		Input("ValidationError", "dag_validation_error").
		Output("BinPath", "final_bin_path").
		Output("Stderr", "final_compile_stderr").

		Vertex("envscan").Op("EnvScanOp").
		Input("GoFiles", "go_files").
		Output("RequiredEnvVars", "required_env_vars").

		Vertex("mcpbprompt").Op("MCPBManifestPromptOp").
		Input("Prompt", "prompt_const_out").
		Input("BinPath", "final_bin_path").
		Input("RequiredEnvVars", "required_env_vars").
		Output("Name", "mcpb_name").
		Output("DisplayName", "mcpb_display_name").
		Output("Description", "mcpb_description").
		Output("Author", "mcpb_author").

		Vertex("package").Op("PackageMCPBOp").
		Input("BinPath", "final_bin_path").
		Input("Name", "mcpb_name").
		Input("DisplayName", "mcpb_display_name").
		Input("Description", "mcpb_description").
		Input("Author", "mcpb_author").
		Input("RequiredEnvVars", "required_env_vars").
		Output("MCPBPath", "mcpb_path").

		Build()
}

func registerDriverOps() {
	operator.RegisterOp[DAGDesignOp]()
	operator.RegisterOp[DAGDesignRefineOp]()
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
	operator.RegisterOp[EnvScanOp]()
	operator.RegisterOp[MCPBManifestPromptOp]()
	operator.RegisterOp[PackageMCPBOp]()
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	// ── Phase 1: Design ──────────────────────────────────────────────────────────
	designDAG, err := buildDesignDAG()
	if err != nil {
		log.Fatalf("buildDesignDAG: %v", err)
	}
	eng1, err := dagor.NewEngine(designDAG, pool)
	if err != nil {
		log.Fatalf("NewEngine (design): %v", err)
	}
	if err := eng1.Run(ctx); err != nil {
		log.Fatalf("design DAG run: %v", err)
	}

	promptRaw, _ := eng1.GetOutput("prompt_out")
	libraryRaw, _ := eng1.GetOutput("library_out")
	designRaw, _ := eng1.GetOutput("design_out")

	promptStr := *(promptRaw.(*string))
	libraryStr := *(libraryRaw.(*string))
	approvedDesign := *(designRaw.(*string))
	eng1.Close(ctx)

	// ── Phase 2: Review loop (up to 3 rounds) ───────────────────────────────────
	reader := bufio.NewReader(os.Stdin)
	for round := 1; round <= 3; round++ {
		fmt.Printf("\n═══ DAG DESIGN (round %d/3) ═══\n\n%s\n", round, approvedDesign)
		fmt.Print("\nPress Enter to approve, or type feedback to refine: ")
		feedback, _ := reader.ReadString('\n')
		feedback = strings.TrimSpace(feedback)

		if feedback == "" || strings.EqualFold(feedback, "y") || strings.EqualFold(feedback, "yes") {
			fmt.Println("Design approved.")
			break
		}

		if round == 3 {
			fmt.Println("Max refinement rounds reached — proceeding with current design.")
			break
		}

		refineDAG, err := buildRefineDAG(promptStr, libraryStr, approvedDesign, feedback)
		if err != nil {
			log.Fatalf("buildRefineDAG: %v", err)
		}
		eng2, err := dagor.NewEngine(refineDAG, pool)
		if err != nil {
			log.Fatalf("NewEngine (refine): %v", err)
		}
		if err := eng2.Run(ctx); err != nil {
			log.Fatalf("refine DAG run: %v", err)
		}
		refinedRaw, _ := eng2.GetOutput("design_out")
		approvedDesign = *(refinedRaw.(*string))
		eng2.Close(ctx)
	}

	// ── Phase 3: Codegen ─────────────────────────────────────────────────────────
	codegenDAG, err := buildCodegenDAG(promptStr, libraryStr, approvedDesign, modulePath)
	if err != nil {
		log.Fatalf("buildCodegenDAG: %v", err)
	}
	log.Printf("[DEBUG] codegen DAG built: %d vertices", codegenDAG.Size())
	for name := range codegenDAG.Vertices() {
		log.Printf("[DEBUG]   vertex: %s", name)
	}
	eng3, err := dagor.NewEngine(codegenDAG, pool)
	if err != nil {
		log.Fatalf("NewEngine (codegen): %v", err)
	}
	runErr := eng3.Run(ctx)
	log.Printf("[DEBUG] codegen DAG run finished, err=%v", runErr)
	log.Printf("[DEBUG] envscan skipped=%v, mcpbprompt skipped=%v, package skipped=%v",
		eng3.VertexSkipped("envscan"), eng3.VertexSkipped("mcpbprompt"), eng3.VertexSkipped("package"))
	if runErr != nil {
		eng3.Close(ctx)
		log.Fatalf("codegen DAG run: %v", runErr)
	}

	binPathRaw, _ := eng3.GetOutput("final_bin_path")
	mcpbRaw, _ := eng3.GetOutput("mcpb_path")
	eng3.Close(ctx)

	binPath := ""
	if binPathRaw != nil {
		binPath = *(binPathRaw.(*string))
	}

	fmt.Printf("\n--- Generated Binary ---\n%s\n", binPath)

	if mcpbRaw != nil {
		if p := *(mcpbRaw.(*string)); p != "" {
			fmt.Printf("\n--- Generated MCPB ---\n%s\n", p)
		}
	}
}
