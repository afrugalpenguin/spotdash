package instance

import (
	"strings"
	"testing"
)

func TestTheLockNameIsPerUserSession(t *testing.T) {
	if name := lockName(`C:\spot\config.json`); !strings.HasPrefix(name, `Local\`) {
		t.Errorf("name = %q, want the Local namespace", name)
	}
}

func TestTheLockNameFollowsTheConfigPathIgnoringCase(t *testing.T) {
	a := lockName(`C:\Spot\Config.json`)

	if b := lockName(`c:\spot\config.JSON`); a != b {
		t.Errorf("names for the same path differ: %q and %q", a, b)
	}
	if c := lockName(`C:\other\config.json`); a == c {
		t.Errorf("different configs share the name %q", a)
	}
}
