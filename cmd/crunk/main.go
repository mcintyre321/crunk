// Command crunk is a CI pipeline interpreter. It evaluates untrusted pipeline
// HCL into closed container specs; it never runs pipeline code itself.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/hashicorp/hcl/v2"
	"github.com/mcintyre321/crunk/internal/lang"
)

const usage = `usage: crunk <command> [flags]

commands:
  validate   load and statically check a pipeline

flags:
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd, args := args[0], args[1:]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root; nothing outside it is read")
	dir := fs.String("dir", ".", "pipeline directory, relative to the root")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	switch cmd {
	case "validate":
		p, diags := lang.Load(*root, *dir, lang.DefaultLimits())
		if diags.HasErrors() {
			var files map[string]*hcl.File
			if p != nil {
				files = p.Files
			}
			_ = hcl.NewDiagnosticTextWriter(stderr, files, 100, false).WriteDiagnostics(diags)
			return 1
		}
		fmt.Fprintln(stdout, "pipeline is valid")
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", cmd, usage)
		fs.PrintDefaults()
		return 2
	}
}
