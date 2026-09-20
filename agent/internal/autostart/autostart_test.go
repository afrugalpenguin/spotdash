package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeStore is the registry value, held in memory.
type fakeStore struct {
	data    string
	present bool
	err     error
	writes  int
}

func (f *fakeStore) Get() (string, bool, error) { return f.data, f.present, f.err }
func (f *fakeStore) Set(data string) error {
	if f.err != nil {
		return f.err
	}
	f.data, f.present = data, true
	f.writes++
	return nil
}
func (f *fakeStore) Delete() error {
	if f.err != nil {
		return f.err
	}
	f.data, f.present = "", false
	return nil
}

// installed is where a real copy of the agent would sit, away from any temp
// directory.
func installed() (exe, temp string) {
	root := filepath.Join(string(filepath.Separator), "apps")
	return filepath.Join(root, "spotdash dir", "spotdash.exe"), filepath.Join(root, "tmp")
}

func TestEnablingWritesTheQuotedAbsolutePathOfTheExecutable(t *testing.T) {
	exe, temp := installed()
	store := &fakeStore{}
	m := New(store, exe, temp)

	if err := m.Set(true); err != nil {
		t.Fatalf("Set(true): %v", err)
	}

	// Quoted, or a path with a space is cut at the space.
	if want := `"` + exe + `"`; store.data != want {
		t.Errorf("value = %q, want %q", store.data, want)
	}
}

func TestDisablingRemovesTheValueAndIsHarmlessWhenThereIsNone(t *testing.T) {
	exe, temp := installed()
	store := &fakeStore{data: `"` + exe + `"`, present: true}
	m := New(store, exe, temp)

	if err := m.Set(false); err != nil {
		t.Fatalf("Set(false): %v", err)
	}
	if store.present {
		t.Error("value exists after Set(false), want none")
	}
	if err := m.Set(false); err != nil {
		t.Errorf("second Set(false): %v", err)
	}
}

func TestEnabledReflectsTheStoreNotAnyRememberedSetting(t *testing.T) {
	exe, temp := installed()
	store := &fakeStore{}
	m := New(store, exe, temp)

	if on, err := m.Enabled(); err != nil || on {
		t.Errorf("with no value Enabled = %v, %v, want false", on, err)
	}

	// Someone runs reg add behind the agent's back.
	store.data, store.present = `"`+exe+`"`, true
	if on, err := m.Enabled(); err != nil || !on {
		t.Errorf("with the value present Enabled = %v, %v, want true", on, err)
	}
}

// After a move, the old value launches nothing. Showing it as ticked would hide
// that, and ticking the item repoints it.
func TestAValueForAnotherPathIsNotThisExecutableBeingEnabled(t *testing.T) {
	exe, temp := installed()
	store := &fakeStore{data: `"` + filepath.Join(string(filepath.Separator), "old", "spotdash.exe") + `"`, present: true}
	m := New(store, exe, temp)

	if on, _ := m.Enabled(); on {
		t.Error("Enabled = true for another path, want false")
	}
	if err := m.Set(true); err != nil {
		t.Fatalf("Set(true): %v", err)
	}
	if want := `"` + exe + `"`; store.data != want {
		t.Errorf("value = %q, want %q", store.data, want)
	}
}

func TestStoreErrorsAreReturned(t *testing.T) {
	exe, temp := installed()
	boom := errors.New("access denied")
	m := New(&fakeStore{err: boom}, exe, temp)

	if _, err := m.Enabled(); !errors.Is(err, boom) {
		t.Errorf("Enabled error = %v, want %v", err, boom)
	}
	if err := m.Set(true); !errors.Is(err, boom) {
		t.Errorf("Set(true) error = %v, want %v", err, boom)
	}
	if err := m.Set(false); !errors.Is(err, boom) {
		t.Errorf("Set(false) error = %v, want %v", err, boom)
	}
}

func TestAnExecutableInTheTempDirectoryCannotBeMadeAutostart(t *testing.T) {
	temp := filepath.Join(string(filepath.Separator), "apps", "tmp")
	exe := filepath.Join(temp, "go-build123", "b001", "exe", "spotdash.exe")
	store := &fakeStore{}
	m := New(store, exe, temp)

	reason := m.Unavailable()

	if reason == "" {
		t.Fatal("Unavailable = \"\" for an executable in the temp directory")
	}
	if !strings.Contains(strings.ToLower(reason), "temporary") {
		t.Errorf("reason = %q, want it to mention temporary", reason)
	}
	if err := m.Set(true); err == nil || store.writes != 0 {
		t.Errorf("Set(true) = %v with %d writes, want an error and 0", err, store.writes)
	}
}

func TestAnInstalledExecutableIsAvailable(t *testing.T) {
	exe, temp := installed()

	if reason := New(&fakeStore{}, exe, temp).Unavailable(); reason != "" {
		t.Errorf("reason = %q, want empty", reason)
	}
}

func TestTheTempCheckDoesNotMatchASiblingDirectoryWithTheSamePrefix(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "apps")
	temp := filepath.Join(root, "tmp")
	exe := filepath.Join(root, "tmp2", "spotdash.exe")

	if reason := New(&fakeStore{}, exe, temp).Unavailable(); reason != "" {
		t.Errorf("reason = %q, want empty (tmp2 is not inside tmp)", reason)
	}
}

func TestTheTempCheckIgnoresCaseOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("path case is only ignored on windows")
	}
	temp := `C:\Users\Me\AppData\Local\Temp`
	exe := `c:\users\me\appdata\local\temp\go-build1\spotdash.exe`

	if reason := New(&fakeStore{}, exe, temp).Unavailable(); reason == "" {
		t.Error("Unavailable = \"\" for a temp path in different case")
	}
}

func TestTheRealTempDirectoryIsWhatGoRunBuildsInto(t *testing.T) {
	exe := filepath.Join(os.TempDir(), "go-build42", "b001", "exe", "spotdash.exe")

	if reason := New(&fakeStore{}, exe, os.TempDir()).Unavailable(); reason == "" {
		t.Error("Unavailable = \"\" for a binary in os.TempDir()")
	}
}
