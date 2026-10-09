package version

import (
	"regexp"
	"testing"
)

func TestCurrentIsPlainSemanticVersion(t *testing.T) {
	if !regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`).MatchString(Current) {
		t.Fatalf("invalid source version %q", Current)
	}
}
