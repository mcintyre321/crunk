package lang

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
)

const corpus = "../../testdata/malicious"

// Each corpus directory and a fragment of the error it must produce.
var maliciousCases = map[string]string{
	"host-exec-stage":                "`stage` was replaced by `container`",
	"host-exec-shell":                "fixed shell",
	"host-exec-args":                 "use `script`",
	"data-terraform":                 "data sources are not supported",
	"data-env":                       "data sources are not supported",
	"data-prompt":                    "data sources are not supported",
	"data-git":                       "data sources are not supported",
	"macro":                          "macros are not supported",
	"import-remote":                  "`import` is not supported",
	"hook-pre":                       "hooks are not supported",
	"hook-post":                      "hooks are not supported",
	"pre-block":                      "hooks are not supported",
	"daemon":                         "lifecycle is derived",
	"module-git":                     "not a local path",
	"module-https":                   "not a local path",
	"module-s3":                      "not a local path",
	"module-gcs":                     "not a local path",
	"module-absolute":                "not a local path",
	"module-dotdot-escape":           "outside the repository root",
	"module-expression-source":       "must be a literal string",
	"module-cycle":                   "calls itself",
	"func-file":                      "functions that read files are not supported",
	"func-templatefile":              "functions that read files are not supported",
	"func-fileexists":                "functions that read files are not supported",
	"func-env":                       "host environment is not visible",
	"func-timestamp":                 "time functions are not supported",
	"func-uuid":                      "random functions are not supported",
	"ref-host-path":                  "host paths are not visible",
	"ref-hostname":                   "the host is not visible",
	"forbidden-volume":               "host volumes are not supported",
	"forbidden-privileged":           "fixed by the platform",
	"forbidden-network":              "fixed by the platform",
	"forbidden-user":                 "fixed by the platform",
	"forbidden-entrypoint":           "fixed by the platform",
	"forbidden-nested-container":     "`stage` was replaced by `container`",
	"port-attribute":                 "`port` is a block, not an argument",
	"unknown-attribute":              "an argument named `colour` is not expected here",
	"unknown-block":                  "blocks of type `pipeline` are not expected here",
	"top-level-attribute":            "an argument named `image` is not expected here",
	"reserved-container-name":        "is a built-in container",
	"ready-ambiguous":                "exactly one of `tcp` or an `http` block",
	"missing-image":                  "the argument `image` is required",
	"dimension-block":                "`dimension` was replaced by `parallel` blocks",
	"container-host-argument":        "put them inside a `host` block",
	"container-for-each":             "put it inside a `parallel` block",
	"module-for-each":                "put it inside a `parallel` block",
	"each-reference":                 "`each` was replaced by the name of the enclosing `parallel` block",
	"parallel-count-and-for-each":    "exactly one of `count` or `for_each`",
	"parallel-no-count":              "exactly one of `count` or `for_each`",
	"parallel-value-with-count":      "only available when the parallel block uses `for_each`",
	"parallel-reserved-name":         "cannot name a parallel block",
	"parallel-shadowed-name":         "nested inside another parallel block with the same name",
	"parallel-missing-index":         "pick one iteration with `parallel.p[<key>]`",
	"parallel-iterator-out-of-scope": "`p` is not a known name",
	"container-out-of-scope":         "refer to it as `parallel.p[<key>].container.inner`",
	"unknown-container":              `there is no container named "nope" in scope`,
	"unknown-port":                   `container "a" has no port named "grpc"`,
	"unknown-variable":               `there is no variable named "registry"`,
	"bare-container-reference":       "refer to `.url`, `.address`, `.files` or `.result`",
	"mount-from-result":              "`from` must be a files output",
	"depends-on-not-container":       "each `depends_on` entry must refer to a container",
	"nested-host":                    "hosts do not nest",
	"parallel-inside-host":           "put the `host` block inside the `parallel` block instead",
	"host-module-declares-host":      "declares hosts of its own",
	"schema-type-and-json":           "exactly one of `type` or `json`",
	"result-schema-not-schema":       "must be a reference to a schema",
	"variable-default-reference":     "variable defaults must be constant",
}

func TestMaliciousCorpus(t *testing.T) {
	entries, err := os.ReadDir(corpus)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, ok := maliciousCases[e.Name()]; !ok {
			t.Errorf("corpus directory %q has no expectation in maliciousCases", e.Name())
		}
	}
	for name, want := range maliciousCases {
		t.Run(name, func(t *testing.T) {
			_, diags := Load(filepath.Join(corpus, name), ".", DefaultLimits())
			requireError(t, diags, want)
		})
	}
}

func TestModuleSymlinkEscape(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	write(t, filepath.Join(outside, "main.hcl"), `container "x" { image = "a/b" }`)
	write(t, filepath.Join(root, "main.hcl"), `module "m" { source = "./link" }`)
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	_, diags := Load(root, ".", DefaultLimits())
	requireError(t, diags, "outside the repository root")
}

func TestFileSymlinkEscape(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	write(t, filepath.Join(outside, "secret.hcl"), `container "x" { image = "a/b" }`)
	if err := os.Symlink(filepath.Join(outside, "secret.hcl"), filepath.Join(root, "main.hcl")); err != nil {
		t.Fatal(err)
	}
	_, diags := Load(root, ".", DefaultLimits())
	requireError(t, diags, "outside the repository root")
}

func TestRootDirEscape(t *testing.T) {
	root := t.TempDir()
	_, diags := Load(root, "..", DefaultLimits())
	requireError(t, diags, "outside the repository root")
}

func TestOversizedFile(t *testing.T) {
	root := t.TempDir()
	limits := DefaultLimits()
	src := "locals {\n  x = \"" + strings.Repeat("a", int(limits.MaxFileBytes)) + "\"\n}\n"
	write(t, filepath.Join(root, "main.hcl"), src)
	_, diags := Load(root, ".", limits)
	requireError(t, diags, "larger than")
}

func TestDeeplyNestedFile(t *testing.T) {
	limits := DefaultLimits()
	for name, depth := range map[string]int{
		"rejected by the pre-parse bound": 10000,
		"rejected after parsing":          limits.MaxNestingDepth,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			src := "locals {\n  x = " + strings.Repeat("[", depth) + strings.Repeat("]", depth) + "\n}\n"
			write(t, filepath.Join(root, "main.hcl"), src)
			_, diags := Load(root, ".", limits)
			requireError(t, diags, "nest")
		})
	}
}

func TestModuleDepthLimit(t *testing.T) {
	root := t.TempDir()
	limits := DefaultLimits()
	dir := root
	for i := 0; i <= limits.MaxModuleDepth+1; i++ {
		write(t, filepath.Join(dir, "main.hcl"), `module "next" { source = "./next" }`)
		dir = filepath.Join(dir, "next")
	}
	write(t, filepath.Join(dir, "main.hcl"), `container "leaf" { image = "a/b" }`)
	_, diags := Load(root, ".", limits)
	requireError(t, diags, "module depth exceeds")
}

func TestTooManyFiles(t *testing.T) {
	root := t.TempDir()
	limits := DefaultLimits()
	limits.MaxFilesPerModule = 2
	for _, n := range []string{"a.hcl", "b.hcl", "c.hcl"} {
		write(t, filepath.Join(root, n), "")
	}
	_, diags := Load(root, ".", limits)
	requireError(t, diags, "more than 2 .hcl files")
}

func requireError(t *testing.T, diags hcl.Diagnostics, want string) {
	t.Helper()
	if !diags.HasErrors() {
		t.Fatalf("expected an error containing %q, got none", want)
	}
	for _, d := range diags {
		if strings.Contains(d.Summary+": "+d.Detail, want) {
			return
		}
	}
	t.Fatalf("expected an error containing %q, got:\n%s", want, diags.Error())
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
