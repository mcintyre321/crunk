// Package lang loads and statically validates pipeline HCL.
//
// Everything read here is untrusted: file names, file contents and module
// sources. Load never evaluates expressions; it checks structure, the
// vocabulary (blocks, arguments, functions, variable roots) and resource
// limits, so that a rejected pipeline fails before anything is dispatched.
package lang

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/typeexpr"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// Limits bound the work Load will do on untrusted input.
type Limits struct {
	MaxFileBytes      int64 // size of one .hcl file
	MaxFilesPerModule int   // .hcl files in one module directory
	MaxTotalFiles     int   // .hcl files across all modules
	MaxModuleDepth    int   // nesting of module calls; the root module is depth 0
	MaxNestingDepth   int   // syntax-tree depth of blocks and expressions in one file
}

func DefaultLimits() Limits {
	return Limits{
		MaxFileBytes:      1 << 20,
		MaxFilesPerModule: 64,
		MaxTotalFiles:     512,
		MaxModuleDepth:    8,
		MaxNestingDepth:   64,
	}
}

// Pipeline is a loaded, structurally valid pipeline.
type Pipeline struct {
	Root   string // absolute repository root with symlinks resolved
	Module *Module
	Files  map[string]*hcl.File // by root-relative name, for diagnostics
}

// Module is one directory of .hcl files.
type Module struct {
	Dir        string // root-relative, slash-separated; "." for the root module
	Containers []*hclsyntax.Block
	Hosts      []*hclsyntax.Block
	Dimensions []*hclsyntax.Block
	Variables  []*hclsyntax.Block
	Locals     []*hclsyntax.Attribute
	Calls      []*ModuleCall
}

// ModuleCall is a `module` block together with the module it loads.
type ModuleCall struct {
	Block  *hclsyntax.Block
	Source string
	Module *Module
}

// BuiltinContainers are container names provided by the platform.
var BuiltinContainers = map[string]bool{"source": true}

// allowedRoots are the only names an expression may start with.
var allowedRoots = map[string]bool{
	"var": true, "local": true, "container": true, "host": true,
	"dimension": true, "each": true, "module": true,
}

var removedRoots = map[string]string{
	"path":     "host paths are not visible to pipelines",
	"cwd":      "host paths are not visible to pipelines",
	"owd":      "host paths are not visible to pipelines",
	"hostname": "the host is not visible to pipelines",
	"hostuser": "the host is not visible to pipelines",
	"env":      "the host environment is not visible to pipelines; pass values with variables",
	"pipeline": "pipeline metadata is not available",
	"data":     "data sources are not supported",
	"stage":    "`stage` was replaced by `container`",
	"macro":    "macros are not supported",
	"param":    "macros are not supported",
	"this":     "`this` is not supported",
	"output":   "outputs are not supported; read `container.<name>.result`",
	"ansi":     "`ansi` is not supported",
}

// Load reads the root module in dir (relative to root) and every module it
// calls. Module sources must be relative paths that resolve, after following
// symlinks, to directories inside root.
func Load(root, dir string, limits Limits) (*Pipeline, hcl.Diagnostics) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, errorf(nil, "Invalid root", "%s", err)
	}
	realRoot, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, errorf(nil, "Invalid root", "%s", err)
	}
	l := &loader{root: realRoot, limits: limits, files: map[string]*hcl.File{}}
	start, diags := l.resolve(realRoot, dir, nil)
	if diags.HasErrors() {
		return nil, diags
	}
	mod, d := l.loadModule(start, 0, nil)
	diags = diags.Extend(d)
	return &Pipeline{Root: realRoot, Module: mod, Files: l.files}, diags
}

type loader struct {
	root   string
	limits Limits
	files  map[string]*hcl.File
	total  int
	stack  []string
}

func (l *loader) rel(p string) string {
	r, err := filepath.Rel(l.root, p)
	if err != nil {
		return p
	}
	return filepath.ToSlash(r)
}

func within(root, p string) bool {
	r, err := filepath.Rel(root, p)
	if err != nil || filepath.IsAbs(r) {
		return false
	}
	return r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

// resolve joins rel onto base, follows symlinks, and requires a directory inside the root.
func (l *loader) resolve(base, rel string, subject *hcl.Range) (string, hcl.Diagnostics) {
	if filepath.IsAbs(rel) {
		return "", errorf(subject, "Invalid module path", "%q is absolute; module paths must be relative", rel)
	}
	real, err := filepath.EvalSymlinks(filepath.Join(base, filepath.FromSlash(rel)))
	if err != nil {
		return "", errorf(subject, "Invalid module path", "%q does not exist", rel)
	}
	if !within(l.root, real) {
		return "", errorf(subject, "Module path escapes the repository", "%q resolves outside the repository root", rel)
	}
	info, err := os.Stat(real)
	if err != nil || !info.IsDir() {
		return "", errorf(subject, "Invalid module path", "%q is not a directory", rel)
	}
	return real, nil
}

func (l *loader) loadModule(dir string, depth int, subject *hcl.Range) (*Module, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	if depth > l.limits.MaxModuleDepth {
		return nil, errorf(subject, "Modules nested too deeply", "module depth exceeds the limit of %d", l.limits.MaxModuleDepth)
	}
	for _, s := range l.stack {
		if s == dir {
			return nil, errorf(subject, "Module cycle", "module %q calls itself", l.rel(dir))
		}
	}
	l.stack = append(l.stack, dir)
	defer func() { l.stack = l.stack[:len(l.stack)-1] }()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, errorf(subject, "Cannot read module", "%s: %s", l.rel(dir), err)
	}
	mod := &Module{Dir: l.rel(dir)}
	count := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".hcl") {
			continue
		}
		count++
		l.total++
		if count > l.limits.MaxFilesPerModule {
			return nil, errorf(subject, "Too many files", "module %q has more than %d .hcl files", mod.Dir, l.limits.MaxFilesPerModule)
		}
		if l.total > l.limits.MaxTotalFiles {
			return nil, errorf(subject, "Too many files", "pipeline has more than %d .hcl files", l.limits.MaxTotalFiles)
		}
		body, d := l.readFile(filepath.Join(dir, e.Name()))
		diags = diags.Extend(d)
		if body != nil {
			diags = diags.Extend(collect(mod, body))
		}
	}
	if count == 0 {
		diags = diags.Extend(errorf(subject, "Empty module", "no .hcl files in %q", mod.Dir))
	}
	diags = diags.Extend(checkNames(mod))
	if diags.HasErrors() {
		return mod, diags
	}
	for _, call := range mod.Calls {
		r := call.Block.Body.Attributes["source"].Expr.Range()
		childDir, d := l.resolve(dir, call.Source, &r)
		diags = diags.Extend(d)
		if d.HasErrors() {
			continue
		}
		call.Module, d = l.loadModule(childDir, depth+1, &r)
		diags = diags.Extend(d)
	}
	return mod, diags
}

func (l *loader) readFile(path string) (*hclsyntax.Body, hcl.Diagnostics) {
	name := l.rel(path)
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, errorf(nil, "Cannot read file", "%s: %s", name, err)
	}
	if !within(l.root, real) {
		return nil, errorf(nil, "File escapes the repository", "%s resolves outside the repository root", name)
	}
	f, err := os.Open(real)
	if err != nil {
		return nil, errorf(nil, "Cannot read file", "%s: %s", name, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errorf(nil, "Cannot read file", "%s is not a regular file", name)
	}
	src, err := io.ReadAll(io.LimitReader(f, l.limits.MaxFileBytes+1))
	if err != nil {
		return nil, errorf(nil, "Cannot read file", "%s: %s", name, err)
	}
	if int64(len(src)) > l.limits.MaxFileBytes {
		return nil, errorf(nil, "File too large", "%s is larger than %d bytes", name, l.limits.MaxFileBytes)
	}
	// The parser is recursive; refuse pathological bracket nesting before parsing.
	if bracketDepth(src) > 4*l.limits.MaxNestingDepth {
		return nil, errorf(nil, "File nested too deeply", "%s exceeds the nesting limit of %d", name, l.limits.MaxNestingDepth)
	}
	file, diags := hclsyntax.ParseConfig(src, name, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}
	l.files[name] = file
	body := file.Body.(*hclsyntax.Body)
	if depth := syntaxDepth(body); depth > l.limits.MaxNestingDepth {
		return nil, errorf(nil, "File nested too deeply", "%s has nesting depth %d; the limit is %d", name, depth, l.limits.MaxNestingDepth)
	}
	return body, nil
}

// collect checks one file's top-level body against the schema and adds its blocks to mod.
func collect(mod *Module, body *hclsyntax.Body) hcl.Diagnostics {
	diags := checkBody(body, topSchema)
	for _, b := range body.Blocks {
		if _, ok := topSchema.blocks[b.Type]; !ok || len(b.Labels) != len(topSchema.blocks[b.Type].labels) {
			continue
		}
		switch b.Type {
		case "container":
			mod.Containers = append(mod.Containers, b)
			diags = diags.Extend(checkContainer(b))
			diags = diags.Extend(checkExprs(b.Body))
		case "host":
			mod.Hosts = append(mod.Hosts, b)
			diags = diags.Extend(checkExprs(b.Body))
		case "dimension":
			mod.Dimensions = append(mod.Dimensions, b)
			diags = diags.Extend(checkExprs(b.Body))
		case "locals":
			for _, a := range sortedAttrs(b.Body) {
				mod.Locals = append(mod.Locals, a)
				diags = diags.Extend(checkExpr(a.Expr))
			}
		case "variable":
			mod.Variables = append(mod.Variables, b)
			for _, a := range sortedAttrs(b.Body) {
				if a.Name == "type" {
					_, d := typeexpr.TypeConstraint(a.Expr)
					diags = diags.Extend(d)
				} else {
					diags = diags.Extend(checkExpr(a.Expr))
				}
			}
		case "module":
			call, d := moduleCall(b)
			diags = diags.Extend(d)
			if call != nil {
				mod.Calls = append(mod.Calls, call)
			}
		}
	}
	return diags
}

func moduleCall(b *hclsyntax.Block) (*ModuleCall, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	for _, a := range sortedAttrs(b.Body) {
		if a.Name != "source" {
			diags = diags.Extend(checkExpr(a.Expr))
		}
	}
	attr, ok := b.Body.Attributes["source"]
	if !ok {
		return nil, diags // reported by checkBody
	}
	r := attr.Expr.Range()
	v, d := attr.Expr.Value(nil)
	if d.HasErrors() || v.Type() != cty.String || v.IsNull() {
		return nil, diags.Extend(errorf(&r, "Invalid module source", "source must be a literal string"))
	}
	src := v.AsString()
	if !strings.HasPrefix(src, "./") && !strings.HasPrefix(src, "../") {
		return nil, diags.Extend(errorf(&r, "Invalid module source",
			"%q is not a local path; module sources must start with ./ or ../ (remote sources such as git, https, s3 and gcs are not supported)", src))
	}
	return &ModuleCall{Block: b, Source: src}, diags
}

func checkContainer(b *hclsyntax.Block) hcl.Diagnostics {
	var diags hcl.Diagnostics
	name := b.Labels[0]
	if BuiltinContainers[name] {
		diags = diags.Extend(errorf(b.LabelRanges[0].Ptr(), "Reserved container name", "%q is a built-in container", name))
	}
	seen := map[string]bool{}
	for _, inner := range b.Body.Blocks {
		if len(inner.Labels) == 1 {
			key := inner.Type + "." + inner.Labels[0]
			if seen[key] {
				diags = diags.Extend(errorf(inner.LabelRanges[0].Ptr(), "Duplicate block", "%s %q is declared more than once", inner.Type, inner.Labels[0]))
			}
			seen[key] = true
		}
		if inner.Type == "ready" {
			_, tcp := inner.Body.Attributes["tcp"]
			http := len(inner.Body.Blocks) > 0
			if tcp == http {
				diags = diags.Extend(errorf(inner.DefRange().Ptr(), "Invalid ready probe", "`ready` needs exactly one of `tcp` or an `http` block"))
			}
		}
	}
	return diags
}

// checkNames reports invalid and duplicate block names within one module.
func checkNames(mod *Module) hcl.Diagnostics {
	var diags hcl.Diagnostics
	seen := map[string]bool{}
	add := func(kind, name string, r hcl.Range) {
		if !hclsyntax.ValidIdentifier(name) {
			diags = diags.Extend(errorf(&r, "Invalid name", "%q is not a valid identifier", name))
		}
		key := kind + "." + name
		if seen[key] {
			diags = diags.Extend(errorf(&r, "Duplicate name", "%s %q is declared more than once", kind, name))
		}
		seen[key] = true
	}
	for _, group := range []struct {
		kind   string
		blocks []*hclsyntax.Block
	}{{"container", mod.Containers}, {"host", mod.Hosts}, {"dimension", mod.Dimensions}, {"variable", mod.Variables}} {
		for _, b := range group.blocks {
			add(group.kind, b.Labels[0], b.LabelRanges[0])
		}
	}
	for _, c := range mod.Calls {
		add("module", c.Block.Labels[0], c.Block.LabelRanges[0])
	}
	for _, a := range mod.Locals {
		add("local", a.Name, a.NameRange)
	}
	return diags
}

// checkBody checks arguments and blocks against s, recursively.
func checkBody(body *hclsyntax.Body, s *schema) hcl.Diagnostics {
	var diags hcl.Diagnostics
	for _, a := range sortedAttrs(body) {
		r := a.NameRange
		if why, ok := s.removed[a.Name]; ok {
			diags = diags.Extend(errorf(&r, "Unsupported argument", "%s", why))
			continue
		}
		if _, ok := s.attrs[a.Name]; ok || s.anyAttrs {
			continue
		}
		if _, ok := s.blocks[a.Name]; ok {
			diags = diags.Extend(errorf(&r, "Unsupported argument", "`%s` is a block, not an argument: write %s", a.Name, blockHint(a.Name, s.blocks[a.Name])))
			continue
		}
		diags = diags.Extend(errorf(&r, "Unsupported argument", "an argument named `%s` is not expected here", a.Name))
	}
	for _, name := range sortedKeys(s.attrs) {
		if _, ok := body.Attributes[name]; s.attrs[name] && !ok {
			r := body.SrcRange
			diags = diags.Extend(errorf(&r, "Missing required argument", "the argument `%s` is required", name))
		}
	}
	counts := map[string]int{}
	for _, b := range body.Blocks {
		r := b.TypeRange
		if why, ok := s.removed[b.Type]; ok {
			diags = diags.Extend(errorf(&r, "Unsupported block type", "%s", why))
			continue
		}
		bs, ok := s.blocks[b.Type]
		if !ok {
			diags = diags.Extend(errorf(&r, "Unsupported block type", "blocks of type `%s` are not expected here", b.Type))
			continue
		}
		if len(b.Labels) != len(bs.labels) {
			diags = diags.Extend(errorf(&r, "Wrong number of labels", "write %s", blockHint(b.Type, bs)))
			continue
		}
		counts[b.Type]++
		if bs.max > 0 && counts[b.Type] > bs.max {
			diags = diags.Extend(errorf(&r, "Duplicate block", "only %d `%s` block is allowed here", bs.max, b.Type))
		}
		diags = diags.Extend(checkBody(b.Body, bs.body))
	}
	return diags
}

func blockHint(name string, bs *blockSchema) string {
	h := name
	for _, l := range bs.labels {
		h += fmt.Sprintf(" \"<%s>\"", l)
	}
	return h + " { ... }"
}

// checkExprs checks every expression in a block body.
func checkExprs(body *hclsyntax.Body) hcl.Diagnostics {
	var diags hcl.Diagnostics
	for _, a := range sortedAttrs(body) {
		diags = diags.Extend(checkExpr(a.Expr))
	}
	for _, b := range body.Blocks {
		diags = diags.Extend(checkExprs(b.Body))
	}
	return diags
}

// checkExpr rejects calls to functions outside the allowlist and references
// to variable roots outside the language.
func checkExpr(expr hclsyntax.Expression) hcl.Diagnostics {
	c := &exprChecker{}
	hclsyntax.Walk(expr, c)
	return c.diags
}

type exprChecker struct {
	diags  hcl.Diagnostics
	scopes []map[string]bool
}

func (c *exprChecker) Enter(n hclsyntax.Node) hcl.Diagnostics {
	switch e := n.(type) {
	case *hclsyntax.FunctionCallExpr:
		if _, ok := Functions[e.Name]; !ok {
			r := e.NameRange
			detail := fmt.Sprintf("there is no function named `%s`", e.Name)
			if why, ok := removedFunctions[e.Name]; ok {
				detail = why
			}
			c.diags = c.diags.Extend(errorf(&r, "Unsupported function", "%s", detail))
		}
	case *hclsyntax.ForExpr:
		scope := map[string]bool{e.ValVar: true}
		if e.KeyVar != "" {
			scope[e.KeyVar] = true
		}
		c.scopes = append(c.scopes, scope)
	case *hclsyntax.ScopeTraversalExpr:
		root := e.Traversal.RootName()
		if allowedRoots[root] || c.inScope(root) {
			break
		}
		r := e.Traversal[0].SourceRange()
		detail := fmt.Sprintf("`%s` is not a known name; expressions can refer to var, local, container, host, dimension, each and module (to write a literal ${...} in a string, such as a shell variable, escape it as $${...})", root)
		if why, ok := removedRoots[root]; ok {
			detail = why
		}
		c.diags = c.diags.Extend(errorf(&r, "Unknown reference", "%s", detail))
	}
	return nil
}

func (c *exprChecker) Exit(n hclsyntax.Node) hcl.Diagnostics {
	if _, ok := n.(*hclsyntax.ForExpr); ok {
		c.scopes = c.scopes[:len(c.scopes)-1]
	}
	return nil
}

func (c *exprChecker) inScope(name string) bool {
	for _, s := range c.scopes {
		if s[name] {
			return true
		}
	}
	return false
}

type depthWalker struct{ depth, max int }

func (w *depthWalker) Enter(hclsyntax.Node) hcl.Diagnostics {
	w.depth++
	if w.depth > w.max {
		w.max = w.depth
	}
	return nil
}

func (w *depthWalker) Exit(hclsyntax.Node) hcl.Diagnostics {
	w.depth--
	return nil
}

func syntaxDepth(body *hclsyntax.Body) int {
	w := &depthWalker{}
	hclsyntax.Walk(body, w)
	return w.max
}

// bracketDepth is a cheap upper bound on nesting, computed before parsing.
func bracketDepth(src []byte) int {
	depth, max := 0, 0
	for _, c := range src {
		switch c {
		case '{', '[', '(':
			depth++
			if depth > max {
				max = depth
			}
		case '}', ']', ')':
			if depth > 0 {
				depth--
			}
		}
	}
	return max
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedAttrs(body *hclsyntax.Body) []*hclsyntax.Attribute {
	attrs := make([]*hclsyntax.Attribute, 0, len(body.Attributes))
	for _, a := range body.Attributes {
		attrs = append(attrs, a)
	}
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].SrcRange.Start.Byte < attrs[j].SrcRange.Start.Byte })
	return attrs
}

func errorf(subject *hcl.Range, summary, format string, args ...any) hcl.Diagnostics {
	return hcl.Diagnostics{{
		Severity: hcl.DiagError,
		Summary:  summary,
		Detail:   fmt.Sprintf(format, args...),
		Subject:  subject,
	}}
}
