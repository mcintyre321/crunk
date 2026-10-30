package lang

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/mcintyre321/crunk/internal/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

func eval(t *testing.T, src string) (cty.Value, hcl.Diagnostics) {
	t.Helper()
	expr, diags := hclsyntax.ParseExpression([]byte(src), "test.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		t.Fatal(diags.Error())
	}
	return expr.Value(&hcl.EvalContext{Functions: Functions})
}

func TestFunctions(t *testing.T) {
	cases := map[string]cty.Value{
		`lines("a\nb\n")`:                  cty.ListVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b")}),
		`lines("a\r\nb")`:                  cty.ListVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b")}),
		`lines("")`:                        cty.ListValEmpty(cty.String),
		`"x\ny".lines().length()`:          cty.NumberIntVal(2),
		`" A ".trimspace().lower()`:        cty.StringVal("a"),
		`length("héllo")`:                  cty.NumberIntVal(5),
		`length([1, 2, 3])`:                cty.NumberIntVal(3),
		`length({ a = 1 })`:                cty.NumberIntVal(1),
		`startswith("crunk", "cr")`:        cty.True,
		`endswith("crunk", "nk")`:          cty.True,
		`strcontains("crunk", "run")`:      cty.True,
		`alltrue([true, true])`:            cty.True,
		`anytrue([false, true])`:           cty.True,
		`alltrue([])`:                      cty.True,
		`sum([1, 2, 3])`:                   cty.NumberIntVal(6),
		`one([])`:                          cty.NullVal(cty.DynamicPseudoType),
		`one(["a"])`:                       cty.StringVal("a"),
		`tostring(1)`:                      cty.StringVal("1"),
		`tonumber("2")`:                    cty.NumberIntVal(2),
		`tolist(["a"])`:                    cty.ListVal([]cty.Value{cty.StringVal("a")}),
		`base64encode("crunk")`:            cty.StringVal("Y3J1bms="),
		`base64decode("Y3J1bms=")`:         cty.StringVal("crunk"),
		`urlencode("a b&c")`:               cty.StringVal("a+b%26c"),
		`sha256("")`:                       cty.StringVal("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"),
		`shellescape("it's")`:              cty.StringVal(`'it'"'"'s'`),
		`jsonencode({ a = 1 })`:            cty.StringVal(`{"a":1}`),
		`replace("a-b-c", "-", "+")`:       cty.StringVal("a+b+c"),
		`length(range(3))`:                 cty.NumberIntVal(3),
		`try(tonumber("x"), 7)`:            cty.NumberIntVal(7),
		`format("%s-%02d", "shard", 3)`:    cty.StringVal("shard-03"),
		`coalesce(null, "b")`:              cty.StringVal("b"),
		`lookup({ a = "x" }, "b", "dflt")`: cty.StringVal("dflt"),
	}
	for src, want := range cases {
		got, diags := eval(t, src)
		if diags.HasErrors() {
			t.Errorf("%s: %s", src, diags.Error())
			continue
		}
		if !got.RawEquals(want) {
			t.Errorf("%s: got %#v, want %#v", src, got, want)
		}
	}
}

func TestFunctionErrors(t *testing.T) {
	for _, src := range []string{
		`range(100000)`,
		`range(0, 1, 0.00001)`,
		`one([1, 2])`,
		`base64decode("not base64!")`,
		`sum([1, null])`,
	} {
		if _, diags := eval(t, src); !diags.HasErrors() {
			t.Errorf("%s: expected an error", src)
		}
	}
}

func TestFunctionsHandleUnknowns(t *testing.T) {
	expr, _ := hclsyntax.ParseExpression([]byte(`length(x)`), "t.hcl", hcl.InitialPos)
	v, diags := expr.Value(&hcl.EvalContext{
		Functions: Functions,
		Variables: map[string]cty.Value{"x": cty.UnknownVal(cty.String)},
	})
	if diags.HasErrors() || v.IsKnown() {
		t.Fatalf("want an unknown result, got %#v %s", v, diags.Error())
	}
}

// LANGUAGE.md is the allowlist: its implemented-function table must match Functions exactly.
func TestLanguageDocListsFunctions(t *testing.T) {
	doc, err := os.ReadFile("../../docs/LANGUAGE.md")
	if err != nil {
		t.Fatal(err)
	}
	section := string(doc)
	start := strings.Index(section, "## Functions")
	if start < 0 {
		t.Fatal("LANGUAGE.md has no Functions section")
	}
	section = section[start:]
	// Stop at the next top-level section, or at the planned (not yet implemented) list.
	for _, marker := range []string{"\n## ", "\n### Planned"} {
		if end := strings.Index(section[3:], marker); end >= 0 {
			section = section[:end+3]
		}
	}
	var documented []string
	row := regexp.MustCompile("(?m)^\\| ([^|]+) \\|")
	name := regexp.MustCompile("`([a-z0-9]+)`")
	for _, m := range row.FindAllStringSubmatch(section, -1) {
		for _, n := range name.FindAllStringSubmatch(m[1], -1) {
			documented = append(documented, n[1])
		}
	}
	var registered []string
	for n := range Functions {
		registered = append(registered, n)
	}
	sort.Strings(documented)
	sort.Strings(registered)
	if strings.Join(documented, ",") != strings.Join(registered, ",") {
		t.Errorf("LANGUAGE.md functions and Functions differ\n doc: %v\n code: %v", documented, registered)
	}
}
