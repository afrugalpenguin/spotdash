package instance

import (
	"strings"
	"testing"
)

func TestTheLockNameIsPerUserSession(t *testing.T) {
	if name := lockName(`C:\spot\config.json`); !strings.HasPrefix(name, `Local\`) {
		t.Errorf("name = %q, want it in the Local namespace so another user's agent does not block this one", name)
	}
}

func TestTheLockNameFollowsTheConfigPathIgnoringCase(t *testing.T) {
	a := lockName(`C:\Spot\Config.json`)

	if b := lockName(`c:\spot\config.JSON`); a != b {
		t.Errorf("the same file spelt with different case gave two names: %q and %q", a, b)
	}
	if c := lockName(`C:\other\config.json`); a == c {
		t.Errorf("two different configs shared one name %q, so a second agent with its own config could not run", a)
	}
}
