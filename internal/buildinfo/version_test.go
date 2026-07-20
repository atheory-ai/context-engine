package buildinfo

import "testing"

func TestVersionIsSet(t *testing.T) {
	if Version == "" {
		t.Fatal("build version must not be empty")
	}
}
