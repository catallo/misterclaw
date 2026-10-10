package version

import (
	"regexp"
	"testing"
)

// Source previews must be distinguishable from published stable releases.
// Numeric prerelease identifiers, like numeric version components, have no leading zero.
var sourceVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?$`)

func TestCurrentIsSemanticSourceVersion(t *testing.T) {
	if !sourceVersionPattern.MatchString(Current) {
		t.Fatalf("invalid source version %q", Current)
	}
}

func TestSourceVersionStableAndPreviewSyntax(t *testing.T) {
	for _, value := range []string{"0.8.0", "0.8.1-zip-preview.20261010", "1.0.0-rc.0"} {
		if !sourceVersionPattern.MatchString(value) {
			t.Errorf("valid stable/preview version rejected: %q", value)
		}
	}
	for _, value := range []string{"v0.8.0", "0.8", "01.8.0", "0.8.1-01", "0.8.1-", "0.8.1-rc..1", "0.8.1\n"} {
		if sourceVersionPattern.MatchString(value) {
			t.Errorf("invalid version accepted: %q", value)
		}
	}
}
