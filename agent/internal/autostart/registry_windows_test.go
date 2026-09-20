//go:build windows

package autostart

import (
	"fmt"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

// scratchStore is a store on a throwaway key, so the real registry code runs
// without ever touching the Run key that decides what starts at login.
func scratchStore(t *testing.T) Store {
	t.Helper()
	path := fmt.Sprintf(`Software\spotdash-autostart-test-%d`, time.Now().UnixNano())
	t.Cleanup(func() { registry.DeleteKey(registry.CURRENT_USER, path) })
	return newRegistryStore(path, ValueName)
}

func TestTheRealStoreTargetsThePerUserRunKey(t *testing.T) {
	if runKeyPath != `Software\Microsoft\Windows\CurrentVersion\Run` {
		t.Errorf("runKeyPath = %q, want the per-user Run key", runKeyPath)
	}
}

func TestRegistryValueRoundTrip(t *testing.T) {
	store := scratchStore(t)

	if data, exists, err := store.Get(); err != nil || exists {
		t.Fatalf("Get on a key that does not exist = %q, %v, %v, want no value and no error", data, exists, err)
	}

	want := `"C:\Program Files\spotdash\spotdash.exe"`
	if err := store.Set(want); err != nil {
		t.Fatalf("Set: %v", err)
	}
	data, exists, err := store.Get()
	if err != nil || !exists || data != want {
		t.Fatalf("Get after Set = %q, %v, %v, want %q", data, exists, err, want)
	}

	if err := store.Set(`"C:\other.exe"`); err != nil {
		t.Fatalf("overwriting: %v", err)
	}
	if data, _, _ := store.Get(); data != `"C:\other.exe"` {
		t.Errorf("value after overwrite = %q", data)
	}

	if err := store.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, exists, _ := store.Get(); exists {
		t.Error("the value is still there after Delete")
	}
}

func TestDeletingWhatIsNotThereIsNotAnError(t *testing.T) {
	store := scratchStore(t)

	if err := store.Delete(); err != nil {
		t.Errorf("Delete with no key: %v", err)
	}
	if err := store.Set("x"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := store.Delete(); err != nil {
		t.Errorf("Delete of an already deleted value: %v", err)
	}
}
