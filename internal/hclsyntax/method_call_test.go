// crunk: tests for method-call syntax.

package hclsyntax

import (
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

func TestMethodCallSyntax(t *testing.T) {
	ctx := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"s": cty.ObjectVal(map[string]cty.Value{"out": cty.StringVal("  A,B  ")}),
		},
		Functions: map[string]function.Function{
			"trimspace": stdlib.TrimSpaceFunc,
			"lower":     stdlib.LowerFunc,
			"split":     stdlib.SplitFunc,
			"length":    stdlib.LengthFunc,
			"join":      stdlib.JoinFunc,
		},
	}
	cases := map[string]cty.Value{
		`s.out.trimspace()`:                                   cty.StringVal("A,B"),
		`s.out.trimspace().lower()`:                           cty.StringVal("a,b"),
		`lower(s.out.trimspace())`:                            cty.StringVal("a,b"),
		`",".split(s.out.trimspace()).length()`:               cty.NumberIntVal(2),
		`"-".join(["x", "y"])`:                                cty.StringVal("x-y"),
		`["x", "y"][0].lower()`:                               cty.StringVal("x"),
		`"${s.out.trimspace()}!"`:                             cty.StringVal("A,B!"),
		`[for p in ",".split(s.out.trimspace()) : p.lower()]`: cty.TupleVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b")}),
	}
	for src, want := range cases {
		var expr Expression
		var diags hcl.Diagnostics
		if strings.HasPrefix(src, `"$`) {
			expr, diags = ParseTemplate([]byte(src[1:len(src)-1]), "t.hcl", hcl.InitialPos)
		} else {
			expr, diags = ParseExpression([]byte(src), "t.hcl", hcl.InitialPos)
		}
		if diags.HasErrors() {
			t.Errorf("%s: parse: %s", src, diags.Error())
			continue
		}
		got, diags := expr.Value(ctx)
		if diags.HasErrors() {
			t.Errorf("%s: eval: %s", src, diags.Error())
			continue
		}
		if !got.RawEquals(want) {
			t.Errorf("%s: got %#v, want %#v", src, got, want)
		}
	}
}

func TestMethodCallShape(t *testing.T) {
	expr, diags := ParseExpression([]byte(`a.b.f(1).g`), "t.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		t.Fatal(diags.Error())
	}
	rel, ok := expr.(*RelativeTraversalExpr)
	if !ok {
		t.Fatalf("want a traversal of the call result, got %T", expr)
	}
	call, ok := rel.Source.(*FunctionCallExpr)
	if !ok || call.Name != "f" || !call.Method || len(call.Args) != 2 {
		t.Fatalf("want f(a.b, 1) marked as a method call, got %#v", rel.Source)
	}
	if _, ok := call.Args[0].(*ScopeTraversalExpr); !ok {
		t.Errorf("receiver should be the traversal a.b, got %T", call.Args[0])
	}
	if r := call.Range(); r.Start.Byte != 0 || r.End.Byte != len("a.b.f(1)") {
		t.Errorf("range should cover the receiver: %#v", r)
	}
}

func TestAttributeWithoutParensIsStillATraversal(t *testing.T) {
	expr, diags := ParseExpression([]byte(`a.length`), "t.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		t.Fatal(diags.Error())
	}
	if _, ok := expr.(*ScopeTraversalExpr); !ok {
		t.Fatalf("got %T", expr)
	}
}
