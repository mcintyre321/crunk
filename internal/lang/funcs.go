package lang

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/alessio/shellescape"
	"github.com/hashicorp/hcl/v2/ext/tryfunc"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

// MaxRangeElements caps the size of a list produced by range().
const MaxRangeElements = 10000

// Functions is the complete set of functions available to pipeline HCL. Every
// function here is pure and deterministic: no filesystem, environment, network,
// clock or randomness. docs/LANGUAGE.md lists the same set; keep them in sync.
var Functions = map[string]function.Function{
	// string
	"chomp":       stdlib.ChompFunc,
	"endswith":    EndsWithFunc,
	"format":      stdlib.FormatFunc,
	"indent":      stdlib.IndentFunc,
	"join":        stdlib.JoinFunc,
	"lines":       LinesFunc,
	"lower":       stdlib.LowerFunc,
	"regex":       stdlib.RegexFunc,
	"regexall":    stdlib.RegexAllFunc,
	"replace":     stdlib.ReplaceFunc,
	"shellescape": ShellEscapeFunc,
	"split":       stdlib.SplitFunc,
	"startswith":  StartsWithFunc,
	"strcontains": StrContainsFunc,
	"substr":      stdlib.SubstrFunc,
	"trim":        stdlib.TrimFunc,
	"trimprefix":  stdlib.TrimPrefixFunc,
	"trimspace":   stdlib.TrimSpaceFunc,
	"trimsuffix":  stdlib.TrimSuffixFunc,
	"upper":       stdlib.UpperFunc,

	// collection
	"alltrue":  AllTrueFunc,
	"anytrue":  AnyTrueFunc,
	"coalesce": stdlib.CoalesceFunc,
	"compact":  stdlib.CompactFunc,
	"concat":   stdlib.ConcatFunc,
	"contains": stdlib.ContainsFunc,
	"distinct": stdlib.DistinctFunc,
	"element":  stdlib.ElementFunc,
	"flatten":  stdlib.FlattenFunc,
	"keys":     stdlib.KeysFunc,
	"length":   LengthFunc,
	"lookup":   stdlib.LookupFunc,
	"merge":    stdlib.MergeFunc,
	"one":      OneFunc,
	"range":    RangeFunc,
	"slice":    stdlib.SliceFunc,
	"sort":     stdlib.SortFunc,
	"values":   stdlib.ValuesFunc,
	"zipmap":   stdlib.ZipmapFunc,

	// numeric
	"abs":      stdlib.AbsoluteFunc,
	"ceil":     stdlib.CeilFunc,
	"floor":    stdlib.FloorFunc,
	"max":      stdlib.MaxFunc,
	"min":      stdlib.MinFunc,
	"parseint": stdlib.ParseIntFunc,
	"sum":      SumFunc,

	// conversion and error handling
	"can":      tryfunc.CanFunc,
	"try":      tryfunc.TryFunc,
	"tobool":   makeToFunc(cty.Bool),
	"tolist":   makeToFunc(cty.List(cty.DynamicPseudoType)),
	"tomap":    makeToFunc(cty.Map(cty.DynamicPseudoType)),
	"tonumber": makeToFunc(cty.Number),
	"toset":    makeToFunc(cty.Set(cty.DynamicPseudoType)),
	"tostring": makeToFunc(cty.String),

	// encoding and hashing
	"base64decode": Base64DecodeFunc,
	"base64encode": Base64EncodeFunc,
	"jsondecode":   stdlib.JSONDecodeFunc,
	"jsonencode":   stdlib.JSONEncodeFunc,
	"sha256":       Sha256Func,
	"urlencode":    URLEncodeFunc,
}

// removedFunctions explains why well-known upstream functions are missing.
var removedFunctions = map[string]string{
	"file":             "functions that read files are not supported; mount files into a container instead",
	"filebase64":       "functions that read files are not supported; mount files into a container instead",
	"fileexists":       "functions that read files are not supported; mount files into a container instead",
	"fileset":          "functions that read files are not supported; mount files into a container instead",
	"filemd5":          "functions that read files are not supported; mount files into a container instead",
	"filesha1":         "functions that read files are not supported; mount files into a container instead",
	"filesha256":       "functions that read files are not supported; mount files into a container instead",
	"filesha512":       "functions that read files are not supported; mount files into a container instead",
	"filebase64sha256": "functions that read files are not supported; mount files into a container instead",
	"filebase64sha512": "functions that read files are not supported; mount files into a container instead",
	"templatefile":     "functions that read files are not supported; mount files into a container instead",
	"abspath":          "path functions are not supported",
	"pathexpand":       "path functions are not supported",
	"basename":         "path functions are not supported",
	"dirname":          "path functions are not supported",
	"env":              "the host environment is not visible to pipelines; pass values with variables",
	"which":            "the host environment is not visible to pipelines",
	"timestamp":        "time functions are not supported: pipelines must evaluate deterministically",
	"timeadd":          "time functions are not supported: pipelines must evaluate deterministically",
	"timecmp":          "time functions are not supported: pipelines must evaluate deterministically",
	"formatdate":       "time functions are not supported: pipelines must evaluate deterministically",
	"uuid":             "random functions are not supported: pipelines must evaluate deterministically",
	"bcrypt":           "random functions are not supported: pipelines must evaluate deterministically",
}

var StartsWithFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "str", Type: cty.String},
		{Name: "prefix", Type: cty.String},
	},
	Type: function.StaticReturnType(cty.Bool),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		return cty.BoolVal(strings.HasPrefix(args[0].AsString(), args[1].AsString())), nil
	},
})

var EndsWithFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "str", Type: cty.String},
		{Name: "suffix", Type: cty.String},
	},
	Type: function.StaticReturnType(cty.Bool),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		return cty.BoolVal(strings.HasSuffix(args[0].AsString(), args[1].AsString())), nil
	},
})

var StrContainsFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "str", Type: cty.String},
		{Name: "substr", Type: cty.String},
	},
	Type: function.StaticReturnType(cty.Bool),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		return cty.BoolVal(strings.Contains(args[0].AsString(), args[1].AsString())), nil
	},
})

// LinesFunc splits text into lines. It accepts \n or \r\n line endings and
// ignores a final line ending, so "a\nb\n" gives ["a", "b"] and "" gives [].
var LinesFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "str", Type: cty.String}},
	Type:   function.StaticReturnType(cty.List(cty.String)),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		text := strings.TrimSuffix(strings.ReplaceAll(args[0].AsString(), "\r\n", "\n"), "\n")
		if text == "" {
			return cty.ListValEmpty(cty.String), nil
		}
		var vals []cty.Value
		for _, line := range strings.Split(text, "\n") {
			vals = append(vals, cty.StringVal(line))
		}
		return cty.ListVal(vals), nil
	},
})

var ShellEscapeFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "str", Type: cty.String}},
	Type:   function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		return cty.StringVal(shellescape.Quote(args[0].AsString())), nil
	},
})

// LengthFunc returns the number of characters in a string, elements in a collection, or attributes of an object.
var LengthFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "value", Type: cty.DynamicPseudoType, AllowDynamicType: true}},
	Type:   function.StaticReturnType(cty.Number),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		switch ty := args[0].Type(); {
		case ty == cty.String:
			return stdlib.Strlen(args[0])
		case ty.IsObjectType():
			return cty.NumberIntVal(int64(len(ty.AttributeTypes()))), nil
		}
		return stdlib.Length(args[0])
	},
})

var AllTrueFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "list", Type: cty.List(cty.Bool)}},
	Type:   function.StaticReturnType(cty.Bool),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		for _, v := range args[0].AsValueSlice() {
			if v.IsNull() || v.False() {
				return cty.False, nil
			}
		}
		return cty.True, nil
	},
})

var AnyTrueFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "list", Type: cty.List(cty.Bool)}},
	Type:   function.StaticReturnType(cty.Bool),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		for _, v := range args[0].AsValueSlice() {
			if !v.IsNull() && v.True() {
				return cty.True, nil
			}
		}
		return cty.False, nil
	},
})

// OneFunc returns the single element of a zero- or one-element list, or null if it is empty.
var OneFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "list", Type: cty.DynamicPseudoType}},
	Type: func(args []cty.Value) (cty.Type, error) {
		ty := args[0].Type()
		switch {
		case ty.IsListType() || ty.IsSetType():
			return ty.ElementType(), nil
		case ty.IsTupleType():
			switch len(ty.TupleElementTypes()) {
			case 0:
				return cty.DynamicPseudoType, nil
			case 1:
				return ty.TupleElementTypes()[0], nil
			}
			return cty.NilType, function.NewArgErrorf(0, "must be a list, set, or tuple value with either zero or one elements")
		}
		return cty.NilType, function.NewArgErrorf(0, "must be a list, set, or tuple value with either zero or one elements")
	},
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		vals := args[0].AsValueSlice()
		switch len(vals) {
		case 0:
			return cty.NullVal(retType), nil
		case 1:
			return vals[0], nil
		}
		return cty.NilVal, function.NewArgErrorf(0, "must be a list, set, or tuple value with either zero or one elements")
	},
})

var SumFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "list", Type: cty.List(cty.Number)}},
	Type:   function.StaticReturnType(cty.Number),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		total := cty.Zero
		for _, v := range args[0].AsValueSlice() {
			if v.IsNull() {
				return cty.NilVal, function.NewArgErrorf(0, "list must not contain null values")
			}
			total = total.Add(v)
		}
		return total, nil
	},
})

// RangeFunc is stdlib range with a cap on the number of elements produced.
var RangeFunc = function.New(&function.Spec{
	VarParam: &function.Parameter{Name: "params", Type: cty.Number},
	Type:     function.StaticReturnType(cty.List(cty.Number)),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		var start, end, step float64 = 0, 0, 1
		get := func(v cty.Value) float64 { f, _ := v.AsBigFloat().Float64(); return f }
		switch len(args) {
		case 1:
			end = get(args[0])
		case 2:
			start, end = get(args[0]), get(args[1])
		case 3:
			start, end, step = get(args[0]), get(args[1]), get(args[2])
		}
		if step != 0 {
			if n := (end - start) / step; n > MaxRangeElements {
				return cty.NilVal, fmt.Errorf("range would produce more than %d elements", MaxRangeElements)
			}
		}
		return stdlib.Range(args...)
	},
})

func makeToFunc(want cty.Type) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{
			Name:             "v",
			Type:             cty.DynamicPseudoType,
			AllowNull:        true,
			AllowDynamicType: true,
		}},
		Type: func(args []cty.Value) (cty.Type, error) {
			got := args[0].Type()
			if got.Equals(want) {
				return got, nil
			}
			conv := convert.GetConversionUnsafe(got, want)
			if conv == nil {
				return cty.NilType, function.NewArgErrorf(0, "cannot convert %s to %s", got.FriendlyName(), want.FriendlyNameForConstraint())
			}
			if want.HasDynamicTypes() {
				// Find the concrete result type, e.g. list(string) for tolist(["a"]).
				if !args[0].IsKnown() {
					return want, nil
				}
				v, err := conv(args[0])
				if err != nil {
					return cty.NilType, function.NewArgError(0, err)
				}
				return v.Type(), nil
			}
			return want, nil
		},
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			v, err := convert.Convert(args[0], retType)
			if err != nil {
				return cty.NilVal, function.NewArgError(0, err)
			}
			return v, nil
		},
	})
}

var Base64EncodeFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "str", Type: cty.String}},
	Type:   function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		return cty.StringVal(base64.StdEncoding.EncodeToString([]byte(args[0].AsString()))), nil
	},
})

var Base64DecodeFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "str", Type: cty.String}},
	Type:   function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		b, err := base64.StdEncoding.DecodeString(args[0].AsString())
		if err != nil {
			return cty.NilVal, function.NewArgErrorf(0, "invalid base64: %s", err)
		}
		if !utf8.Valid(b) {
			return cty.NilVal, function.NewArgErrorf(0, "decoded bytes are not valid UTF-8")
		}
		return cty.StringVal(string(b)), nil
	},
})

var URLEncodeFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "str", Type: cty.String}},
	Type:   function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		return cty.StringVal(url.QueryEscape(args[0].AsString())), nil
	},
})

var Sha256Func = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "str", Type: cty.String}},
	Type:   function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		sum := sha256.Sum256([]byte(args[0].AsString()))
		return cty.StringVal(hex.EncodeToString(sum[:])), nil
	},
})
