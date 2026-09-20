//go:build windows

package instance

import "testing"

func TestASecondAcquireOfTheSameKeyIsRefused(t *testing.T) {
	key := `C:\test\` + t.Name() + `\config.json`
	release, ok, err := Acquire(key)
	if err != nil || !ok {
		t.Fatalf("first Acquire = %v, %v, want true, nil", ok, err)
	}
	defer release()

	_, second, err := Acquire(key)

	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if second {
		t.Error("second Acquire of a held key succeeded, want refusal")
	}
}

func TestReleasingLetsTheKeyBeTakenAgain(t *testing.T) {
	key := `C:\test\` + t.Name() + `\config.json`
	release, ok, _ := Acquire(key)
	if !ok {
		t.Fatal("first Acquire failed")
	}
	release()

	again, ok, err := Acquire(key)

	if err != nil || !ok {
		t.Fatalf("Acquire after release = %v, %v, want true, nil", ok, err)
	}
	again()
}

func TestDifferentKeysDoNotBlockEachOther(t *testing.T) {
	a, ok, _ := Acquire(`C:\test\` + t.Name() + `\a\config.json`)
	if !ok {
		t.Fatal("first Acquire failed")
	}
	defer a()

	b, ok, err := Acquire(`C:\test\` + t.Name() + `\b\config.json`)

	if err != nil || !ok {
		t.Fatalf("Acquire of a different key = %v, %v, want true, nil", ok, err)
	}
	b()
}
