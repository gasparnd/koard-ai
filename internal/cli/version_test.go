package cli

import (
	"bytes"
	"testing"
)

func TestReleaseVersionAliases(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		t.Run(arg, func(t *testing.T) {
			var out, err bytes.Buffer
			if code := Run([]string{arg}, t.TempDir(), &out, &err); code != 0 || out.String() != "0.1.0\n" || err.Len() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), err.String())
			}
		})
	}
}
