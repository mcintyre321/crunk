package lang

import (
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/typeexpr"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// Module is one directory of .hcl files.
type Module struct {
	Dir       string // root-relative, slash-separated; "." for the root module
	Root      *Scope
	Variables []*hclsyntax.Block
	Schemas   []*hclsyntax.Block
}

// Scope is a naming scope: a module's top level, or one `parallel` block.
// `host` blocks do not create scopes; they only decide placement.
type Scope struct {
	Parent     *Scope
	Parallel   *hclsyntax.Block // nil for a module's top level
	Containers []*Container
	Parallels  []*Scope
	Hosts      []*hclsyntax.Block
	Calls      []*ModuleCall
	Locals     []*hclsyntax.Attribute
}

// Container is a `container` block and where it sits.
type Container struct {
	Block *hclsyntax.Block
	Host  *hclsyntax.Block // nil = the current host
	Scope *Scope
}

// ModuleCall is a `module` block together with the module it loads.
type ModuleCall struct {
	Block  *hclsyntax.Block
	Source string
	Host   *hclsyntax.Block // the enclosing host block, if any
	Scope  *Scope
	Module *Module
}

func (c *Container) Name() string { return c.Block.Labels[0] }

// Name is the parallel block's label; empty for a module's top level.
func (s *Scope) Name() string {
	if s.Parallel == nil {
		return ""
	}
	return s.Parallel.Labels[0]
}

func (s *Scope) container(name string) *Container {
	for _, c := range s.Containers {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

func (s *Scope) parallel(name string) *Scope {
	for _, p := range s.Parallels {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

func (s *Scope) call(name string) *ModuleCall {
	for _, c := range s.Calls {
		if c.Block.Labels[0] == name {
			return c
		}
	}
	return nil
}

func (s *Scope) local(name string) bool {
	for _, a := range s.Locals {
		if a.Name == name {
			return true
		}
	}
	return false
}

// AllCalls returns every module call in the module, in every scope.
func (m *Module) AllCalls() []*ModuleCall {
	var calls []*ModuleCall
	var walk func(*Scope)
	walk = func(s *Scope) {
		calls = append(calls, s.Calls...)
		for _, p := range s.Parallels {
			walk(p)
		}
	}
	walk(m.Root)
	return calls
}

// declaresHosts reports whether the module, or any module it calls, has a host block.
func (m *Module) declaresHosts() bool {
	var walk func(*Scope) bool
	walk = func(s *Scope) bool {
		if len(s.Hosts) > 0 {
			return true
		}
		for _, p := range s.Parallels {
			if walk(p) {
				return true
			}
		}
		for _, c := range s.Calls {
			if c.Module != nil && c.Module.declaresHosts() {
				return true
			}
		}
		return false
	}
	return walk(m.Root)
}

// BuiltinContainers are container names provided by the platform.
var BuiltinContainers = map[string]bool{"source": true}

// reservedNames cannot be used as parallel block names, because a parallel
// block's name becomes an expression root (`shard.index`).
var reservedNames = map[string]bool{
	"var": true, "local": true, "container": true, "parallel": true, "module": true,
	"schema": true, "host": true, "each": true, "self": true, "count": true,
}

// collect checks one file's top-level body against the schema and adds its blocks to mod.
func collect(mod *Module, body *hclsyntax.Body) hcl.Diagnostics {
	diags := checkBody(body, topSchema)
	for _, b := range body.Blocks {
		switch b.Type {
		case "variable":
			if len(b.Labels) == 1 {
				mod.Variables = append(mod.Variables, b)
				if a, ok := b.Body.Attributes["type"]; ok {
					_, d := typeexpr.TypeConstraint(a.Expr)
					diags = diags.Extend(d)
				}
			}
		case "schema":
			if len(b.Labels) == 1 {
				mod.Schemas = append(mod.Schemas, b)
				diags = diags.Extend(checkSchemaBlock(b))
			}
		}
	}
	diags = diags.Extend(collectScope(mod.Root, body, nil))
	return diags
}

// collectScope adds containers, parallels, hosts, modules and locals from body to s.
// Structure was already checked by checkBody, so malformed blocks are skipped here.
func collectScope(s *Scope, body *hclsyntax.Body, host *hclsyntax.Block) hcl.Diagnostics {
	var diags hcl.Diagnostics
	for _, b := range body.Blocks {
		bs, ok := blockSchemaFor(b.Type)
		if !ok || len(b.Labels) != len(bs.labels) {
			continue
		}
		switch b.Type {
		case "container":
			s.Containers = append(s.Containers, &Container{Block: b, Host: host, Scope: s})
			diags = diags.Extend(checkContainer(b))
		case "parallel":
			if host != nil {
				continue // parallel inside host is reported by checkBody
			}
			child := &Scope{Parent: s, Parallel: b}
			s.Parallels = append(s.Parallels, child)
			diags = diags.Extend(checkParallel(b))
			diags = diags.Extend(collectScope(child, b.Body, nil))
		case "host":
			if host != nil {
				continue // nested hosts are reported by checkBody
			}
			s.Hosts = append(s.Hosts, b)
			diags = diags.Extend(collectScope(s, b.Body, b))
		case "module":
			call, d := moduleCall(b)
			diags = diags.Extend(d)
			if call != nil {
				call.Host, call.Scope = host, s
				s.Calls = append(s.Calls, call)
			}
		case "locals":
			s.Locals = append(s.Locals, sortedAttrs(b.Body)...)
		}
	}
	return diags
}

func blockSchemaFor(typ string) (*blockSchema, bool) {
	switch typ {
	case "container", "parallel", "host", "module", "locals":
		return topSchema.blocks[typ], true
	}
	return nil, false
}

func moduleCall(b *hclsyntax.Block) (*ModuleCall, hcl.Diagnostics) {
	attr, ok := b.Body.Attributes["source"]
	if !ok {
		return nil, nil // reported by checkBody
	}
	r := attr.Expr.Range()
	v, d := attr.Expr.Value(nil)
	if d.HasErrors() || v.Type() != cty.String || v.IsNull() {
		return nil, errorf(&r, "Invalid module source", "source must be a literal string")
	}
	src := v.AsString()
	if !strings.HasPrefix(src, "./") && !strings.HasPrefix(src, "../") {
		return nil, errorf(&r, "Invalid module source",
			"%q is not a local path; module sources must start with ./ or ../ (remote sources such as git, https, s3 and gcs are not supported)", src)
	}
	return &ModuleCall{Block: b, Source: src}, nil
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

func checkParallel(b *hclsyntax.Block) hcl.Diagnostics {
	var diags hcl.Diagnostics
	name := b.Labels[0]
	if reservedNames[name] {
		diags = diags.Extend(errorf(b.LabelRanges[0].Ptr(), "Reserved name", "%q cannot name a parallel block, because the name is used in expressions (`%s.index`)", name, name))
	}
	_, count := b.Body.Attributes["count"]
	_, forEach := b.Body.Attributes["for_each"]
	if count == forEach {
		diags = diags.Extend(errorf(b.DefRange().Ptr(), "Invalid parallel block", "a `parallel` block needs exactly one of `count` or `for_each`"))
	}
	return diags
}

func checkSchemaBlock(b *hclsyntax.Block) hcl.Diagnostics {
	typ, hasType := b.Body.Attributes["type"]
	_, hasJSON := b.Body.Attributes["json"]
	if hasType == hasJSON {
		return errorf(b.DefRange().Ptr(), "Invalid schema", "a `schema` block needs exactly one of `type` or `json`")
	}
	if hasType {
		_, d := typeexpr.TypeConstraint(typ.Expr)
		return d
	}
	return nil
}

// checkNames reports invalid and duplicate names in every scope of mod, and
// parallel names that shadow an enclosing parallel block.
func checkNames(mod *Module) hcl.Diagnostics {
	var diags hcl.Diagnostics
	add := func(seen map[string]bool, kind, name string, r hcl.Range) {
		if !hclsyntax.ValidIdentifier(name) {
			diags = diags.Extend(errorf(&r, "Invalid name", "%q is not a valid identifier", name))
		}
		key := kind + "." + name
		if seen[key] {
			diags = diags.Extend(errorf(&r, "Duplicate name", "%s %q is declared more than once in this scope", kind, name))
		}
		seen[key] = true
	}
	top := map[string]bool{}
	for _, v := range mod.Variables {
		add(top, "variable", v.Labels[0], v.LabelRanges[0])
	}
	for _, s := range mod.Schemas {
		add(top, "schema", s.Labels[0], s.LabelRanges[0])
	}
	var walk func(s *Scope)
	walk = func(s *Scope) {
		seen := map[string]bool{}
		for _, c := range s.Containers {
			add(seen, "container", c.Name(), c.Block.LabelRanges[0])
		}
		for _, h := range s.Hosts {
			add(seen, "host", h.Labels[0], h.LabelRanges[0])
		}
		for _, c := range s.Calls {
			add(seen, "module", c.Block.Labels[0], c.Block.LabelRanges[0])
		}
		for _, a := range s.Locals {
			add(seen, "local", a.Name, a.NameRange)
		}
		for _, p := range s.Parallels {
			add(seen, "parallel", p.Name(), p.Parallel.LabelRanges[0])
			for up := s; up != nil; up = up.Parent {
				if up.Name() == p.Name() {
					r := p.Parallel.LabelRanges[0]
					diags = diags.Extend(errorf(&r, "Shadowed name", "parallel %q is nested inside another parallel block with the same name", p.Name()))
				}
			}
			walk(p)
		}
	}
	walk(mod.Root)
	return diags
}
