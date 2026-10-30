package lang

import (
	"testing"
)

func TestLoadValidPipeline(t *testing.T) {
	p, diags := Load("../../testdata/valid/fanout", ".", DefaultLimits())
	if diags.HasErrors() {
		t.Fatal(diags.Error())
	}
	root := p.Module.Root
	if got := len(root.Containers); got != 4 {
		t.Errorf("top-level containers: got %d, want 4 (build, db, api, report)", got)
	}
	if db := root.container("db"); db == nil || db.Host == nil || db.Host.Labels[0] != "services" {
		t.Errorf("db should be in scope at the top level and placed on host services")
	}
	project := root.parallel("project")
	if project == nil {
		t.Fatal("parallel project missing")
	}
	if test := project.container("test"); test == nil || test.Host == nil || test.Host.Labels[0] != "runner" {
		t.Errorf("test should be in project's scope and placed on host runner")
	}
	if project.parallel("retry") == nil || len(project.Locals) != 1 {
		t.Errorf("nested parallel or scoped local missing")
	}
	calls := p.Module.AllCalls()
	if len(calls) != 1 || calls[0].Module == nil || calls[0].Module.Dir != "modules/stack" {
		t.Fatalf("module call not loaded: %+v", calls)
	}
	if calls[0].Scope != root.parallel("region") {
		t.Errorf("module call should belong to the region scope")
	}
}

func TestDuplicateNamesAcrossFiles(t *testing.T) {
	root := t.TempDir()
	write(t, root+"/a.hcl", `container "x" { image = "a/b" }`)
	write(t, root+"/b.hcl", `container "x" { image = "a/b" }`)
	_, diags := Load(root, ".", DefaultLimits())
	requireError(t, diags, `container "x" is declared more than once`)
}

func TestSameNameInDifferentScopes(t *testing.T) {
	root := t.TempDir()
	write(t, root+"/main.hcl", `
container "x" { image = "a/b" }
parallel "p" {
  count = 2
  container "x" {
    image = "a/b"
    env   = { N = tostring(p.index) }
  }
}`)
	if _, diags := Load(root, ".", DefaultLimits()); diags.HasErrors() {
		t.Fatal(diags.Error())
	}
}

func TestInnerScopeShadowsOuter(t *testing.T) {
	root := t.TempDir()
	// Inside p, container.x is p's own x, which has port "inner" but not "outer".
	write(t, root+"/main.hcl", `
container "x" {
  image = "a/b"
  port "outer" { container = 1 }
}
parallel "p" {
  count = 2
  container "x" {
    image = "a/b"
    port "inner" { container = 1 }
  }
  container "y" {
    image = "a/b"
    env   = { U = container.x.url.outer }
  }
}`)
	_, diags := Load(root, ".", DefaultLimits())
	requireError(t, diags, `container "x" has no port named "outer"`)
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

func TestVariableDefaultsAreConstant(t *testing.T) {
	root := t.TempDir()
	write(t, root+"/main.hcl", `
variable "a" { default = 1 }
variable "b" { default = var.a }`)
	_, diags := Load(root, ".", DefaultLimits())
	requireError(t, diags, "variable defaults must be constant")
}
