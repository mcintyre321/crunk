package lang

import (
	"testing"
)

func TestLoadValidPipeline(t *testing.T) {
	p, diags := Load("../../testdata/valid/fanout", ".", DefaultLimits())
	if diags.HasErrors() {
		t.Fatal(diags.Error())
	}
	m := p.Module
	if got := len(m.Containers); got != 4 {
		t.Errorf("containers: got %d, want 4", got)
	}
	if len(m.Hosts) != 1 || len(m.Dimensions) != 2 || len(m.Variables) != 1 || len(m.Locals) != 1 {
		t.Errorf("unexpected block counts: hosts=%d dimensions=%d variables=%d locals=%d",
			len(m.Hosts), len(m.Dimensions), len(m.Variables), len(m.Locals))
	}
	if len(m.Calls) != 1 || m.Calls[0].Module == nil {
		t.Fatalf("module call not loaded")
	}
	if got := m.Calls[0].Module.Dir; got != "modules/stack" {
		t.Errorf("module dir: got %q", got)
	}
	if got := len(m.Calls[0].Module.Containers); got != 2 {
		t.Errorf("module containers: got %d, want 2", got)
	}
}

func TestDuplicateNamesAcrossFiles(t *testing.T) {
	root := t.TempDir()
	write(t, root+"/a.hcl", `container "x" { image = "a/b" }`)
	write(t, root+"/b.hcl", `container "x" { image = "a/b" }`)
	_, diags := Load(root, ".", DefaultLimits())
	requireError(t, diags, `container "x" is declared more than once`)
}

func TestForExpressionIteratorsAreInScope(t *testing.T) {
	root := t.TempDir()
	write(t, root+"/main.hcl", `locals {
  m = { for k, v in { a = 1 } : k => v * 2 }
  bad = [for x in [1] : y]
}`)
	_, diags := Load(root, ".", DefaultLimits())
	if len(diags) != 1 {
		t.Fatalf("want exactly one error (for y), got:\n%s", diags.Error())
	}
	requireError(t, diags, "`y` is not a known name")
}

func TestInvalidVariableType(t *testing.T) {
	root := t.TempDir()
	write(t, root+"/main.hcl", `variable "v" { type = file("/etc/passwd") }`)
	_, diags := Load(root, ".", DefaultLimits())
	if !diags.HasErrors() {
		t.Fatal("expected an error for a function call in a type constraint")
	}
}

func TestShellInterpolationNeedsEscaping(t *testing.T) {
	root := t.TempDir()
	write(t, root+"/main.hcl", `container "c" {
  image  = "a/b"
  script = "echo $${HOME} ${HOME}"
}`)
	_, diags := Load(root, ".", DefaultLimits())
	if len(diags) != 1 {
		t.Fatalf("want one error for the unescaped ${HOME}, got:\n%s", diags.Error())
	}
	requireError(t, diags, "escape it as $${...}")
}
