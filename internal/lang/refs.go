package lang

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

var removedRoots = map[string]string{
	"each":      "`each` was replaced by the name of the enclosing `parallel` block, for example `shard.index`",
	"dimension": "`dimension` was replaced by `parallel` blocks",
	"host":      "hosts are not referenced; containers are placed by nesting them in a `host` block",
	"path":      "host paths are not visible to pipelines",
	"cwd":       "host paths are not visible to pipelines",
	"owd":       "host paths are not visible to pipelines",
	"hostname":  "the host is not visible to pipelines",
	"hostuser":  "the host is not visible to pipelines",
	"env":       "the host environment is not visible to pipelines; pass values with variables",
	"pipeline":  "pipeline metadata is not available",
	"data":      "data sources are not supported",
	"stage":     "`stage` was replaced by `container`",
	"macro":     "macros are not supported",
	"param":     "macros are not supported",
	"this":      "`this` is not supported",
	"output":    "outputs are not supported; read `container.<name>.result`",
	"ansi":      "`ansi` is not supported",
}

// checkModuleRefs checks every expression in mod, and in the modules it calls.
func checkModuleRefs(mod *Module) hcl.Diagnostics {
	var diags hcl.Diagnostics
	for _, v := range mod.Variables {
		for _, a := range sortedAttrs(v.Body) {
			if a.Name != "type" {
				diags = diags.Extend(checkExpr(a.Expr, refContext{mod: mod}))
			}
		}
	}
	for _, s := range mod.Schemas {
		if a, ok := s.Body.Attributes["json"]; ok {
			diags = diags.Extend(checkExpr(a.Expr, refContext{mod: mod, scope: mod.Root}))
		}
	}
	diags = diags.Extend(checkScopeRefs(mod, mod.Root))
	for _, call := range mod.AllCalls() {
		if call.Module == nil {
			continue
		}
		if call.Host != nil && call.Module.declaresHosts() {
			r := call.Block.DefRange()
			diags = diags.Extend(errorf(&r, "Nested hosts", "module %q is called inside host %q but declares hosts of its own; hosts do not nest",
				call.Block.Labels[0], call.Host.Labels[0]))
		}
		diags = diags.Extend(checkModuleRefs(call.Module))
	}
	return diags
}

func checkScopeRefs(mod *Module, s *Scope) hcl.Diagnostics {
	var diags hcl.Diagnostics
	ctx := refContext{mod: mod, scope: s}
	for _, a := range s.Locals {
		diags = diags.Extend(checkExpr(a.Expr, ctx))
	}
	for _, call := range s.Calls {
		for _, a := range sortedAttrs(call.Block.Body) {
			if a.Name != "source" {
				diags = diags.Extend(checkExpr(a.Expr, ctx))
			}
		}
	}
	for _, c := range s.Containers {
		diags = diags.Extend(checkContainerRefs(ctx, c))
	}
	for _, p := range s.Parallels {
		// count and for_each are evaluated outside the block they repeat.
		for _, a := range sortedAttrs(p.Parallel.Body) {
			diags = diags.Extend(checkExpr(a.Expr, ctx))
		}
		diags = diags.Extend(checkScopeRefs(mod, p))
	}
	return diags
}

func checkContainerRefs(ctx refContext, c *Container) hcl.Diagnostics {
	var diags hcl.Diagnostics
	for _, a := range sortedAttrs(c.Block.Body) {
		switch a.Name {
		case "depends_on":
			bare := ctx
			bare.allowBare = true
			diags = diags.Extend(checkExpr(a.Expr, bare))
			diags = diags.Extend(checkDependsOn(a.Expr))
		case "result_schema":
			diags = diags.Extend(checkExpr(a.Expr, ctx))
			if root, steps, _, ok := flatten(a.Expr); !ok || root != "schema" || len(steps) != 1 {
				r := a.Expr.Range()
				diags = diags.Extend(errorf(&r, "Invalid result_schema", "`result_schema` must be a reference to a schema, such as `schema.build_result`"))
			}
		default:
			diags = diags.Extend(checkExpr(a.Expr, ctx))
		}
	}
	for _, b := range c.Block.Body.Blocks {
		diags = diags.Extend(checkBlockExprs(ctx, b))
		if b.Type == "mount" {
			if from, ok := b.Body.Attributes["from"]; ok {
				diags = diags.Extend(checkMountFrom(from.Expr))
			}
		}
	}
	return diags
}

func checkBlockExprs(ctx refContext, b *hclsyntax.Block) hcl.Diagnostics {
	var diags hcl.Diagnostics
	for _, a := range sortedAttrs(b.Body) {
		diags = diags.Extend(checkExpr(a.Expr, ctx))
	}
	for _, inner := range b.Body.Blocks {
		diags = diags.Extend(checkBlockExprs(ctx, inner))
	}
	return diags
}

// checkDependsOn requires a list of container references.
func checkDependsOn(expr hclsyntax.Expression) hcl.Diagnostics {
	r := expr.Range()
	tuple, ok := expr.(*hclsyntax.TupleConsExpr)
	if !ok {
		return errorf(&r, "Invalid depends_on", "`depends_on` must be a list of container references, such as `[container.build]`")
	}
	var diags hcl.Diagnostics
	for _, e := range tuple.Exprs {
		root, steps, _, ok := flatten(e)
		path := append([]step{{name: root}}, steps...)
		if !ok || len(path) < 2 || path[len(path)-2].name != "container" {
			er := e.Range()
			diags = diags.Extend(errorf(&er, "Invalid depends_on", "each `depends_on` entry must refer to a container, such as `container.build` or `parallel.shard[*].container.test`"))
		}
	}
	return diags
}

// checkMountFrom requires a snapshot: a files output, or subdir(...) of one.
func checkMountFrom(expr hclsyntax.Expression) hcl.Diagnostics {
	if call, ok := expr.(*hclsyntax.FunctionCallExpr); ok && call.Name == "subdir" {
		return nil
	}
	_, steps, _, ok := flatten(expr)
	if ok && len(steps) >= 2 && steps[len(steps)-2].name == "files" {
		return nil
	}
	r := expr.Range()
	return errorf(&r, "Invalid mount source", "`from` must be a files output, such as `container.build.files.out`, or `subdir(...)` of one")
}

type refContext struct {
	mod       *Module
	scope     *Scope // nil where nothing may be referenced (variable defaults)
	allowBare bool   // a bare container reference is allowed (depends_on)
}

// checkExpr rejects calls to functions outside the allowlist and references
// that don't resolve to something visible from ctx.
func checkExpr(expr hclsyntax.Expression, ctx refContext) hcl.Diagnostics {
	c := &refChecker{ctx: ctx, consumed: map[hclsyntax.Node]bool{}}
	hclsyntax.Walk(expr, c)
	return c.diags
}

type refChecker struct {
	ctx      refContext
	diags    hcl.Diagnostics
	forVars  []map[string]bool
	consumed map[hclsyntax.Node]bool
}

func (c *refChecker) errorf(r hcl.Range, summary, format string, args ...any) {
	c.diags = c.diags.Extend(errorf(&r, summary, format, args...))
}

func (c *refChecker) Enter(n hclsyntax.Node) hcl.Diagnostics {
	switch e := n.(type) {
	case *hclsyntax.FunctionCallExpr:
		if _, ok := Functions[e.Name]; !ok {
			detail := fmt.Sprintf("there is no function named `%s`", e.Name)
			if why, ok := removedFunctions[e.Name]; ok {
				detail = why
			}
			c.errorf(e.NameRange, "Unsupported function", "%s", detail)
		}
	case *hclsyntax.ForExpr:
		vars := map[string]bool{e.ValVar: true}
		if e.KeyVar != "" {
			vars[e.KeyVar] = true
		}
		c.forVars = append(c.forVars, vars)
	case hclsyntax.Expression:
		if c.consumed[n] {
			break
		}
		root, steps, nodes, ok := flatten(e)
		if !ok {
			break
		}
		for _, node := range nodes {
			c.consumed[node] = true
		}
		c.checkRef(root, e.Range(), steps)
	}
	return nil
}

func (c *refChecker) Exit(n hclsyntax.Node) hcl.Diagnostics {
	if _, ok := n.(*hclsyntax.ForExpr); ok {
		c.forVars = c.forVars[:len(c.forVars)-1]
	}
	return nil
}

func (c *refChecker) inFor(name string) bool {
	for _, vars := range c.forVars {
		if vars[name] {
			return true
		}
	}
	return false
}

// enclosingParallel finds the parallel block named name that encloses the current scope.
func (c *refChecker) enclosingParallel(name string) *Scope {
	for s := c.ctx.scope; s != nil; s = s.Parent {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

func (c *refChecker) checkRef(root string, r hcl.Range, steps []step) {
	if c.inFor(root) {
		return
	}
	if c.ctx.scope == nil {
		c.errorf(r, "Invalid reference", "`%s` cannot be referenced here; variable defaults must be constant", root)
		return
	}
	if p := c.enclosingParallel(root); p != nil {
		if len(steps) == 0 || !(steps[0].name == "index" || steps[0].name == "key" || steps[0].name == "value") {
			c.errorf(r, "Invalid reference", "`%s` is a parallel block; use `%s.index`, `%s.key` or `%s.value`", root, root, root, root)
			return
		}
		if steps[0].name == "value" {
			if _, ok := p.Parallel.Body.Attributes["for_each"]; !ok {
				c.errorf(r, "Invalid reference", "`%s.value` is only available when the parallel block uses `for_each`", root)
			}
		}
		return
	}
	switch root {
	case "var", "schema":
		if len(steps) == 0 || steps[0].name == "" {
			c.errorf(r, "Invalid reference", "`%s` must be followed by a name, such as `%s.example`", root, root)
			return
		}
		blocks := c.ctx.mod.Variables
		if root == "schema" {
			blocks = c.ctx.mod.Schemas
		}
		for _, b := range blocks {
			if b.Labels[0] == steps[0].name {
				return
			}
		}
		kind := map[string]string{"var": "variable", "schema": "schema"}[root]
		c.errorf(r, "Unknown reference", "there is no %s named %q in this module", kind, steps[0].name)
	case "local", "container", "parallel", "module":
		var search []*Scope
		for s := c.ctx.scope; s != nil; s = s.Parent {
			search = append(search, s)
		}
		c.checkPath(r, search, true, append([]step{{name: root}}, steps...))
	default:
		detail := fmt.Sprintf("`%s` is not a known name; expressions can refer to var, local, container, parallel, module, schema and the names of enclosing parallel blocks (to write a literal ${...} in a string, such as a shell variable, escape it as $${...})", root)
		if why, ok := removedRoots[root]; ok {
			detail = why
		}
		c.errorf(r, "Unknown reference", "%s", detail)
	}
}

// checkPath resolves steps such as container.x..., parallel.p[k]..., module.m...
// against scopes, innermost first. outward is false once the path has moved into
// a specific parallel block or module, where only that scope is searched.
func (c *refChecker) checkPath(r hcl.Range, search []*Scope, outward bool, steps []step) {
	kind := steps[0].name
	if len(steps) < 2 || steps[1].name == "" {
		c.errorf(r, "Invalid reference", "`%s` must be followed by a name", kind)
		return
	}
	name, rest := steps[1].name, steps[2:]
	switch kind {
	case "local":
		for _, s := range search {
			if s.local(name) {
				return
			}
		}
		c.errorf(r, "Unknown reference", "there is no local named %q in scope", name)
	case "container":
		if outward && BuiltinContainers[name] {
			c.checkContainerTail(r, nil, name, rest)
			return
		}
		for _, s := range search {
			if ct := s.container(name); ct != nil {
				c.checkContainerTail(r, ct, name, rest)
				return
			}
		}
		c.errorf(r, "Unknown reference", "there is no container named %q in scope%s", name, hiddenHint(search, name))
	case "parallel":
		var p *Scope
		for _, s := range search {
			if p = s.parallel(name); p != nil {
				break
			}
		}
		if p == nil {
			c.errorf(r, "Unknown reference", "there is no parallel block named %q in scope", name)
			return
		}
		if len(rest) == 0 || !(rest[0].index || rest[0].splat) {
			c.errorf(r, "Missing index", "`parallel.%s` repeats; pick one iteration with `parallel.%s[<key>]` or all of them with `parallel.%s[*]`", name, name, name)
			return
		}
		rest = rest[1:]
		if len(rest) == 0 {
			if !c.ctx.allowBare {
				c.errorf(r, "Invalid reference", "refer to something inside the parallel block, such as `parallel.%s[*].container.<name>`", name)
			}
			return
		}
		c.checkInner(r, []*Scope{p}, rest)
	case "module":
		var call *ModuleCall
		for _, s := range search {
			if call = s.call(name); call != nil {
				break
			}
		}
		if call == nil {
			c.errorf(r, "Unknown reference", "there is no module named %q in scope", name)
			return
		}
		if call.Module == nil {
			return // the module failed to load; that error is already reported
		}
		if len(rest) == 0 {
			c.errorf(r, "Invalid reference", "refer to something inside the module, such as `module.%s.container.<name>`", name)
			return
		}
		c.checkInner(r, []*Scope{call.Module.Root}, rest)
	default:
		c.errorf(r, "Invalid reference", "expected `container`, `parallel` or `module` here, not `%s`", kind)
	}
}

func (c *refChecker) checkInner(r hcl.Range, search []*Scope, steps []step) {
	if steps[0].name != "container" && steps[0].name != "parallel" && steps[0].name != "module" {
		c.errorf(r, "Invalid reference", "expected `container`, `parallel` or `module` here, not `%s`", steps[0].name)
		return
	}
	c.checkPath(r, search, false, steps)
}

func (c *refChecker) checkContainerTail(r hcl.Range, ct *Container, name string, steps []step) {
	if len(steps) == 0 {
		if !c.ctx.allowBare {
			c.errorf(r, "Invalid reference", "refer to `.url`, `.address`, `.files` or `.result` of container %q", name)
		}
		return
	}
	attr := steps[0].name
	if ct == nil { // built-in source container
		if attr == "files" && len(steps) >= 2 && steps[1].name == "tree" {
			return
		}
		c.errorf(r, "Invalid reference", "the built-in container %q only provides `files.tree`", name)
		return
	}
	switch attr {
	case "result":
		return
	case "url", "address", "files":
		blockType := map[string]string{"url": "port", "address": "port", "files": "files"}[attr]
		if len(steps) < 2 || steps[1].name == "" {
			if len(steps) >= 2 && steps[1].index {
				return // dynamic key; checked when evaluated
			}
			c.errorf(r, "Invalid reference", "`%s` must be followed by a %s name", attr, blockType)
			return
		}
		for _, b := range ct.Block.Body.Blocks {
			if b.Type == blockType && len(b.Labels) == 1 && b.Labels[0] == steps[1].name {
				return
			}
		}
		c.errorf(r, "Unknown reference", "container %q has no %s named %q", name, blockType, steps[1].name)
	default:
		c.errorf(r, "Invalid reference", "containers provide `url`, `address`, `files` and `result`, not `%s`", attr)
	}
}

// hiddenHint suggests the explicit form when name exists inside a parallel block
// that is not an enclosing scope.
func hiddenHint(search []*Scope, name string) string {
	var paths []string
	var find func(s *Scope, prefix string)
	find = func(s *Scope, prefix string) {
		for _, p := range s.Parallels {
			path := prefix + "parallel." + p.Name() + "[<key>]."
			if p.container(name) != nil {
				paths = append(paths, path+"container."+name)
			}
			find(p, path)
		}
	}
	for _, s := range search {
		find(s, "")
	}
	if len(paths) == 0 {
		return ""
	}
	return fmt.Sprintf("; it is inside a parallel block, so refer to it as `%s` (or `[*]` for all iterations)", paths[0])
}

// step is one element of a reference path: an attribute name, an index, or [*].
type step struct {
	name  string // attribute name, or a literal string index
	index bool
	splat bool
}

// flatten turns a chain of traversals, indexes and splats into a root name and
// steps, and returns the syntax nodes that make up the chain. Index keys are not
// part of the chain; they are checked as expressions in their own right.
func flatten(expr hclsyntax.Expression) (string, []step, []hclsyntax.Node, bool) {
	switch e := expr.(type) {
	case *hclsyntax.ScopeTraversalExpr:
		return e.Traversal.RootName(), traversalSteps(e.Traversal[1:]), []hclsyntax.Node{e}, true
	case *hclsyntax.RelativeTraversalExpr:
		root, steps, nodes, ok := flatten(e.Source)
		if !ok {
			return "", nil, nil, false
		}
		return root, append(steps, traversalSteps(e.Traversal)...), append(nodes, e), true
	case *hclsyntax.IndexExpr:
		root, steps, nodes, ok := flatten(e.Collection)
		if !ok {
			return "", nil, nil, false
		}
		s := step{index: true}
		if lit, ok := e.Key.(*hclsyntax.TemplateExpr); ok && lit.IsStringLiteral() {
			v, _ := lit.Value(nil)
			s.name = v.AsString()
		}
		return root, append(steps, s), append(nodes, e), true
	case *hclsyntax.SplatExpr:
		root, steps, nodes, ok := flatten(e.Source)
		if !ok {
			return "", nil, nil, false
		}
		steps = append(steps, step{splat: true})
		nodes = append(nodes, e)
		each, eachNodes, ok := flattenRelative(e.Each)
		if !ok {
			return "", nil, nil, false
		}
		return root, append(steps, each...), append(nodes, eachNodes...), true
	}
	return "", nil, nil, false
}

// flattenRelative flattens the per-element part of a splat, which starts at the
// anonymous splat symbol.
func flattenRelative(expr hclsyntax.Expression) ([]step, []hclsyntax.Node, bool) {
	switch e := expr.(type) {
	case *hclsyntax.AnonSymbolExpr:
		return nil, []hclsyntax.Node{e}, true
	case *hclsyntax.RelativeTraversalExpr:
		steps, nodes, ok := flattenRelative(e.Source)
		if !ok {
			return nil, nil, false
		}
		return append(steps, traversalSteps(e.Traversal)...), append(nodes, e), true
	case *hclsyntax.IndexExpr:
		steps, nodes, ok := flattenRelative(e.Collection)
		if !ok {
			return nil, nil, false
		}
		return append(steps, step{index: true}), append(nodes, e), true
	}
	return nil, nil, false
}

func traversalSteps(t hcl.Traversal) []step {
	steps := make([]step, 0, len(t))
	for _, tr := range t {
		switch tt := tr.(type) {
		case hcl.TraverseAttr:
			steps = append(steps, step{name: tt.Name})
		case hcl.TraverseIndex:
			s := step{index: true}
			if tt.Key.Type() == cty.String && tt.Key.IsKnown() && !tt.Key.IsNull() {
				s.name = tt.Key.AsString()
			}
			steps = append(steps, s)
		case hcl.TraverseSplat:
			steps = append(steps, step{splat: true})
		}
	}
	return steps
}
