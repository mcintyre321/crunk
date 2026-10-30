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
	msgPlatform = "container security and runtime settings are fixed by the platform and cannot be configured in a pipeline"
	msgHooks    = "hooks are not supported; use a separate container with `when = \"always\"` or `when = \"on_failure\"`"
	msgDerived  = "lifecycle is derived: give the container a `port`, and it is kept alive while containers that reference it run"
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
		"host":          false,
		"for_each":      false,
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
		"if":             "conditions are not supported; use `for_each` over a dimension of size 0 or 1",
		"container":      "image settings go directly on the `container` block",
		"name":           "the block label is the container's name",
		"ports":          "declare each port as a named block: port \"<name>\" { container = <number> }",
	},
}

var topSchema = &schema{
	blocks: map[string]*blockSchema{
		"container": {labels: []string{"name"}, body: containerSchema},
		"host":      {labels: []string{"name"}, body: &schema{attrs: map[string]bool{"for_each": false}}},
		"dimension": {labels: []string{"name"}, body: &schema{attrs: map[string]bool{"size": true}}},
		"locals":    {body: &schema{anyAttrs: true}},
		"variable": {labels: []string{"name"}, body: &schema{attrs: map[string]bool{
			"description": false, "default": false, "type": false,
		}}},
		"module": {labels: []string{"name"}, body: &schema{
			attrs:    map[string]bool{"source": true, "for_each": false},
			anyAttrs: true,
		}},
	},
	removed: map[string]string{
		"stage":    "`stage` was replaced by `container`",
		"data":     "data sources are not supported; compute the value in a container and read `container.<name>.result`",
		"macro":    "macros are not supported; use a module",
		"import":   "`import` is not supported; use a local `module`",
		"pre":      msgHooks,
		"post":     msgHooks,
		"provider": "providers are not supported",
		"output":   "outputs are not supported; containers expose `result`, `files`, `url` and `address`",
	},
}
