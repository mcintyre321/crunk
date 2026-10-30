// Package lang loads and statically validates pipeline HCL.
//
// Everything read here is untrusted: file names, file contents and module
// sources. Load never evaluates expressions; it checks structure, the
// vocabulary (blocks, arguments, functions), scopes and references, and
// resource limits, so that a rejected pipeline fails before anything is
// dispatched.
package lang

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/mcintyre321/crunk/internal/hclsyntax"
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
	if mod != nil && !diags.HasErrors() {
		// References are checked once every module is loaded, because a
		// reference can reach into a called module.
		diags = diags.Extend(checkModuleRefs(mod))
	}
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
	mod := &Module{Dir: l.rel(dir), Root: &Scope{}}
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
	for _, call := range mod.AllCalls() {
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
