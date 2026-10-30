package lang

// The pipeline vocabulary as a static schema. docs/LANGUAGE.md is the prose form
// of this file; anything not listed here is rejected.

type schema struct {
	attrs    map[string]bool // name -> required
	anyAttrs bool            // locals and module inputs accept any argument name
	blocks   map[string]*blockSchema
	removed  map[string]string // name -> why it is not supported
}

type blockSchema struct {
	labels []string // label names, used in error messages
	max    int      // maximum occurrences in one body; 0 = unlimited
	body   *schema
}

const (
	msgPlatform  = "container security and runtime settings are fixed by the platform and cannot be configured in a pipeline"
	msgHooks     = "hooks are not supported; use a separate container with `when = \"always\"` or `when = \"on_failure\"`"
	msgDerived   = "lifecycle is derived: give the container a `port`, and it is kept alive while containers that reference it run"
	msgRepeat    = "to repeat something, put it inside a `parallel` block"
	msgPlacement = "to place containers on their own host, put them inside a `host` block"
)

var portSchema = &schema{attrs: map[string]bool{"container": true}}

var readySchema = &schema{
	attrs: map[string]bool{"tcp": false},
	blocks: map[string]*blockSchema{
		"http": {max: 1, body: &schema{attrs: map[string]bool{"port": true, "path": false}}},
	},
}

var containerSchema = &schema{
	attrs: map[string]bool{
		"image":         true,
		"script":        false,
		"env":           false,
		"input":         false,
		"result_schema": false,
		"size":          false,
		"timeout":       false,
		"when":          false,
		"allow_failure": false,
		"depends_on":    false,
	},
	blocks: map[string]*blockSchema{
		"port":  {labels: []string{"name"}, body: portSchema},
		"ready": {max: 1, body: readySchema},
		"files": {labels: []string{"name"}, body: &schema{attrs: map[string]bool{"path": true}}},
		"mount": {labels: []string{"path"}, body: &schema{attrs: map[string]bool{"from": true, "mode": false}}},
	},
	removed: map[string]string{
		"host":           msgPlacement,
		"for_each":       msgRepeat,
		"count":          msgRepeat,
		"volume":         "host volumes are not supported; declare `files` on the producer and `mount` them here",
		"volumes":        "host volumes are not supported; declare `files` on the producer and `mount` them here",
		"skip_workspace": "there is no implicit workspace mount; `mount` the source tree from `container.source.files.tree`",
		"privileged":     msgPlatform,
		"network":        msgPlatform,
		"network_mode":   msgPlatform,
		"user":           msgPlatform,
		"entrypoint":     msgPlatform,
		"cap_add":        msgPlatform,
		"cap_drop":       msgPlatform,
		"capabilities":   msgPlatform,
		"devices":        msgPlatform,
		"security_opt":   msgPlatform,
		"workdir":        msgPlatform,
		"stdin":          "containers cannot read from the terminal",
		"shell":          "the script is always run by the platform's fixed shell",
		"args":           "use `script`, or omit it to run the image's default command",
		"command":        "use `script`, or omit it to run the image's default command",
		"dir":            "use `cd` in `script`",
		"pre_hook":       msgHooks,
		"post_hook":      msgHooks,
		"daemon":         msgDerived,
		"lifecycle":      msgDerived,
		"retry":          "retries are not supported",
		"use":            "macros are not supported; use a module",
		"if":             "conditions are not supported; use a `parallel` block with `count = 0` or `1`",
		"container":      "image settings go directly on the `container` block",
		"name":           "the block label is the container's name",
		"ports":          "declare each port as a named block: port \"<name>\" { container = <number> }",
	},
}

var (
	containerBlock = &blockSchema{labels: []string{"name"}, body: containerSchema}
	localsBlock    = &blockSchema{body: &schema{anyAttrs: true}}
	moduleBlock    = &blockSchema{labels: []string{"name"}, body: &schema{
		attrs:    map[string]bool{"source": true},
		anyAttrs: true,
		removed: map[string]string{
			"for_each":   msgRepeat,
			"count":      msgRepeat,
			"depends_on": "modules have no dependencies of their own; reference containers instead",
		},
	}}
	hostBlock = &blockSchema{labels: []string{"name"}, body: &schema{
		blocks: map[string]*blockSchema{"container": containerBlock, "module": moduleBlock, "locals": localsBlock},
		removed: map[string]string{
			"for_each": "to get one host per iteration, put the `host` block inside a `parallel` block",
			"count":    "to get one host per iteration, put the `host` block inside a `parallel` block",
			"host":     "hosts do not nest",
			"parallel": "put the `host` block inside the `parallel` block instead",
		},
	}}
	parallelBlock = &blockSchema{labels: []string{"name"}}
)

func init() {
	// parallel blocks nest, so this schema refers to itself.
	parallelBlock.body = &schema{
		attrs: map[string]bool{"count": false, "for_each": false, "min": false, "max": false},
		blocks: map[string]*blockSchema{
			"parallel": parallelBlock, "host": hostBlock, "container": containerBlock,
			"module": moduleBlock, "locals": localsBlock,
		},
	}
}

var topSchema = &schema{
	blocks: map[string]*blockSchema{
		"container": containerBlock,
		"parallel":  parallelBlock,
		"host":      hostBlock,
		"module":    moduleBlock,
		"locals":    localsBlock,
		"variable": {labels: []string{"name"}, body: &schema{attrs: map[string]bool{
			"description": false, "default": false, "type": false,
		}}},
		"schema": {labels: []string{"name"}, body: &schema{attrs: map[string]bool{"type": false, "json": false}}},
	},
	removed: map[string]string{
		"dimension": "`dimension` was replaced by `parallel` blocks: put what should repeat inside `parallel \"<name>\" { count = <n> ... }`",
		"stage":     "`stage` was replaced by `container`",
		"data":      "data sources are not supported; compute the value in a container and read `container.<name>.result`",
		"macro":     "macros are not supported; use a module",
		"import":    "`import` is not supported; use a local `module`",
		"pre":       msgHooks,
		"post":      msgHooks,
		"provider":  "providers are not supported",
		"output":    "outputs are not supported; containers expose `result`, `files`, `url` and `address`",
	},
}
