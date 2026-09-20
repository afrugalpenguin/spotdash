package config

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// place writes an empty config.json into dir and returns its path.
func place(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveOrder(t *testing.T) {
	root := t.TempDir()
	exeDir := filepath.Join(root, "exe")
	workDir := filepath.Join(root, "work")
	userDir := filepath.Join(root, "user")
	flagFile := filepath.Join(root, "elsewhere", FileName)

	// Nothing exists anywhere: the per-user path is where one gets created.
	loc, err := Resolve("", exeDir, workDir, userDir)
	if err != nil {
		t.Fatalf("Resolve with nothing present: %v", err)
	}
	if want := filepath.Join(userDir, UserDirName, FileName); loc.Path != want || loc.Exists {
		t.Errorf("nothing present: got %+v, want {%s false}", loc, want)
	}

	// Per-user file is used when it is the only one.
	perUser := place(t, filepath.Join(userDir, UserDirName))
	loc, _ = Resolve("", exeDir, workDir, userDir)
	if loc.Path != perUser || !loc.Exists {
		t.Errorf("per-user only: got %+v, want {%s true}", loc, perUser)
	}

	// The working directory beats the per-user file.
	inWork := place(t, workDir)
	loc, _ = Resolve("", exeDir, workDir, userDir)
	if loc.Path != inWork || !loc.Exists {
		t.Errorf("working directory: got %+v, want {%s true}", loc, inWork)
	}

	// Beside the executable beats the working directory, so an existing setup
	// keeps working.
	beside := place(t, exeDir)
	loc, _ = Resolve("", exeDir, workDir, userDir)
	if loc.Path != beside || !loc.Exists {
		t.Errorf("beside the exe: got %+v, want {%s true}", loc, beside)
	}

	// The flag beats everything, whether or not its file exists, and is never
	// replaced by a search.
	loc, _ = Resolve(flagFile, exeDir, workDir, userDir)
	if loc.Path != flagFile || loc.Exists {
		t.Errorf("flag to a missing file: got %+v, want {%s false}", loc, flagFile)
	}
	placed := place(t, filepath.Dir(flagFile))
	if placed != flagFile {
		t.Fatalf("test setup: placed %s, want %s", placed, flagFile)
	}
	loc, _ = Resolve(flagFile, exeDir, workDir, userDir)
	if loc.Path != flagFile || !loc.Exists {
		t.Errorf("flag to an existing file: got %+v, want {%s true}", loc, flagFile)
	}
}

func TestResolveWithNowhereToCreateIsAnError(t *testing.T) {
	root := t.TempDir()
	_, err := Resolve("", filepath.Join(root, "exe"), filepath.Join(root, "work"), "")
	if err == nil {
		t.Fatal("Resolve returned no error with no config found and no per-user directory")
	}
	if !strings.Contains(err.Error(), "-config") {
		t.Errorf("error %q should tell the user to pass -config", err)
	}
}

func TestResolveTreatsAnUnreadableFileAsPresent(t *testing.T) {
	// Anything that is not a clear "does not exist" must never lead to a
	// replacement. A directory named config.json is the simple stand-in: it is
	// there, Load will refuse it, and a first run must not paper over it.
	root := t.TempDir()
	exeDir := filepath.Join(root, "exe")
	if err := os.MkdirAll(filepath.Join(exeDir, FileName), 0o700); err != nil {
		t.Fatal(err)
	}
	loc, err := Resolve("", exeDir, filepath.Join(root, "work"), filepath.Join(root, "user"))
	if err != nil {
		t.Fatal(err)
	}
	if !loc.Exists {
		t.Errorf("an existing entry named %s was reported as absent, so it could be replaced", FileName)
	}
}

func TestGenerateTokenIsStrongAndTypeable(t *testing.T) {
	alnum := regexp.MustCompile(`^[A-Za-z0-9]{32}$`)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		tok, err := GenerateToken()
		if err != nil {
			t.Fatal(err)
		}
		if !alnum.MatchString(tok) {
			t.Fatalf("token %q is not 32 characters of [A-Za-z0-9]", tok)
		}
		if seen[tok] {
			t.Fatalf("token %q repeated", tok)
		}
		seen[tok] = true
	}
}

const exampleForTest = `{
  "listen": "0.0.0.0:8765",
  "token": "` + PlaceholderToken + `",
  "log_level": "info",
  "sources": {
    "clock": {"enabled": true, "interval_ms": 1000},
    "spotify": {"enabled": false, "interval_ms": 5000, "mode": "api", "state_file": "spotify_state.json"}
  }
}`

func TestCreateDefaultWritesALoadableConfigWithAFreshToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", FileName)

	if err := CreateDefault(path, []byte(exampleForTest)); err != nil {
		t.Fatalf("CreateDefault: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("the generated config does not load: %v", err)
	}
	if cfg.Token == PlaceholderToken || len(cfg.Token) != 32 {
		t.Errorf("token = %q, want a generated 32 character token", cfg.Token)
	}
	if cfg.Sources["spotify"].Enabled {
		t.Error("spotify must stay disabled in a generated config")
	}
	if !strings.Contains(string(cfg.Sources["spotify"].Settings), "spotify_state.json") {
		t.Error("the spotify block lost its state_file")
	}

	// Two configs must not share a token.
	other := filepath.Join(t.TempDir(), FileName)
	if err := CreateDefault(other, []byte(exampleForTest)); err != nil {
		t.Fatal(err)
	}
	cfg2, err := Load(other)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token == cfg2.Token {
		t.Error("two generated configs got the same token")
	}
}

func TestCreateDefaultNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	original := []byte(`{"token": "mine, hand written"`) // malformed on purpose
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	err := CreateDefault(path, []byte(exampleForTest))
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("CreateDefault over an existing file = %v, want os.ErrExist", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(original) {
		t.Errorf("the existing file was changed: %q", got)
	}
}

func TestCreateDefaultLeavesNothingBehindOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)

	// An example that is not a valid config makes the create fail.
	if err := CreateDefault(path, []byte(`{"not_a_key": 1}`)); err == nil {
		t.Fatal("CreateDefault accepted an example that is not a config")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("a failed create left files behind: %v", entries)
	}
}
