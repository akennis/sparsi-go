//go:generate go run ./tools/genskills/main.go
//go:generate daggen -type=DAGDesignOp -output=driver_dagdesign_gen.go
//go:generate daggen -type=DAGDesignRefineOp -output=driver_dagdesignrefine_gen.go
//go:generate daggen -type=PromptOp -output=driver_prompt_gen.go
//go:generate daggen -type=LibraryScanOp -output=driver_libscan_gen.go
//go:generate daggen -type=GenerateOp -output=driver_generate_gen.go
//go:generate daggen -type=WriteFilesOp -output=driver_writefiles_gen.go
//go:generate daggen -type=CodegenOp -output=driver_codegen_gen.go
//go:generate daggen -type=CompileOp -output=driver_compile_gen.go
//go:generate daggen -type=RunOp -output=driver_run_gen.go
//go:generate daggen -type=OutputOp -output=driver_output_gen.go
//go:generate daggen -type=FallbackOp -output=driver_fallback_gen.go
//go:generate daggen -type=ValidateDAGOp -output=driver_validate_gen.go
//go:generate daggen -type=EnvScanOp -output=driver_envscan_gen.go
//go:generate daggen -type=MCPBManifestAIOp -output=driver_mcpbmanifestai_gen.go
//go:generate daggen -type=MCPBManifestPromptOp -output=driver_mcpbmanifestprompt_gen.go
//go:generate daggen -type=PackageMCPBOp -output=driver_packagemcpb_gen.go
package main
