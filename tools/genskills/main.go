//go:build ignore

// genskills assembles the skills/ distribution directory from canonical sources:
//   - skill-src/README.md                                            → verbatim copy
//   - skill-src/<skill>/SKILL.md                                     → verbatim copy
//   - skill-src/<skill>/references/examples/README.md                → verbatim copy
//   - skill-src/clawdag-design/references/design-rules.md            → verbatim copy
//   - skill-src/clawdag-codegen/references/dagor-api.md              → verbatim copy
//   - examples/<name>/main.go                                        → examples/<name>.go (with //go:build ignore prepended)
//   - library.AllDescriptions()                                      → library.md (per skill)
//
// Run via: go generate .
// Or directly: go run ./tools/genskills/main.go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	dagailib "github.com/akennis/clawdag-go/library"
)

var skillNames = []string{"clawdag-design", "clawdag-codegen"}

var exampleDirs = []string{
	"ticket-triager",
	"recipe-analyzer",
	"readme-quality",
	"weather-advisor",
	"hn-topic-brief",
	"faithful-summary",
	"local-mcp-server",
	"remote-mcp-server",
	"with-repair",
}

func main() {
	mustCopy("skill-src/README.md", "skills/README.md")

	for _, skill := range skillNames {
		mustCopy(
			filepath.Join("skill-src", skill, "SKILL.md"),
			filepath.Join("skills", skill, "SKILL.md"),
		)
		mustCopy(
			filepath.Join("skill-src", skill, "references", "examples", "README.md"),
			filepath.Join("skills", skill, "references", "examples", "README.md"),
		)
		libContent := "# Available Library Ops\n\n" + dagailib.AllDescriptions() + "\n"
		mustWrite(filepath.Join("skills", skill, "references", "library.md"), []byte(libContent))
	}

	mustCopy(
		"skill-src/clawdag-design/references/design-rules.md",
		"skills/clawdag-design/references/design-rules.md",
	)
	mustCopy(
		"skill-src/clawdag-codegen/references/dagor-api.md",
		"skills/clawdag-codegen/references/dagor-api.md",
	)

	for _, exDir := range exampleDirs {
		src := filepath.Join("examples", exDir, "main.go")
		body, err := os.ReadFile(src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s: %v\n", src, err)
			os.Exit(1)
		}
		tagged := []byte("//go:build ignore\n\n" + string(body))
		for _, skill := range skillNames {
			dst := filepath.Join("skills", skill, "references", "examples", exDir+".go")
			mustWrite(dst, tagged)
		}
	}
}

func mustCopy(src, dst string) {
	body, err := os.ReadFile(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", src, err)
		os.Exit(1)
	}
	mustWrite(dst, body)
}

func mustWrite(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", filepath.Dir(path), err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", path)
}
