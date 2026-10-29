package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"validate", "--root", "../../testdata/valid/fanout"}, &out, &errOut); code != 0 {
		t.Fatalf("valid pipeline: exit %d\n%s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	code := run([]string{"validate", "--root", "../../testdata/malicious/func-file"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("malicious pipeline: exit %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "main.hcl line 3") {
		t.Errorf("diagnostic should point at the source:\n%s", errOut.String())
	}
}

func TestUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != 2 {
		t.Errorf("no command: exit %d, want 2", code)
	}
	if code := run([]string{"run"}, &out, &errOut); code != 2 {
		t.Errorf("unknown command: exit %d, want 2", code)
	}
}
