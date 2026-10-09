package run

import (
	"reflect"
	"testing"
)

func TestNewCovdataCommandUsesGoToolPrefix(t *testing.T) {
	cmd := newCovdataCommand("/usr/local/go/bin/go", "subtract", "-i=in", "-o=out")

	want := []string{"/usr/local/go/bin/go", "tool", "covdata", "subtract", "-i=in", "-o=out"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("command args = %v, want %v", cmd.Args, want)
	}
}

func TestNewCovdataCommandUsesStandaloneCovdataBinary(t *testing.T) {
	cmd := newCovdataCommand("/tmp/covdata-modified", "textfmt", "-i=in", "-o=out")

	want := []string{"/tmp/covdata-modified", "textfmt", "-i=in", "-o=out"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("command args = %v, want %v", cmd.Args, want)
	}
}
