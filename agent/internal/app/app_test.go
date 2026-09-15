package app

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// freePort returns a port nothing is listening on. There is a small race
// between closing and rebinding, which is acceptable in a test and avoids
// teaching the config loader about port zero purely for testing.
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func writeConfig(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
}

func configFor(port int, token string) string {
	return fmt.Sprintf(`{
		"listen": "127.0.0.1:%d",
		"token": %q,
		"log_level": "error",
		"sources": {"clock": {"enabled": true, "interval_ms": 1000}}
	}`, port, token)
}

// startApp writes a config, starts the agent, and stops it when the test ends.
func startApp(t *testing.T, contents string) (*App, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	writeConfig(t, path, contents)

	agent := New(path, "test-version", discardLogger())
	if err := agent.Start(); err != nil {
		t.Fatalf("Start returned an error: %v", err)
	}
	t.Cleanup(func() { _ = agent.Stop() })
	return agent, path
}

func get(t *testing.T, url, token string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestStartServesHealth(t *testing.T) {
	port := freePort(t)
	agent, _ := startApp(t, configFor(port, "first-token"))

	if got := get(t, agent.baseURL()+"/health", ""); got != http.StatusOK {
		t.Errorf("GET /health = %d, want 200", got)
	}
}

func TestStartRefusesAnInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeConfig(t, path, `{"token": "", "sources": {}}`)

	agent := New(path, "test-version", discardLogger())
	err := agent.Start()

	if err == nil {
		_ = agent.Stop()
		t.Fatal("Start should refuse an empty token")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error should name the offending key, got: %v", err)
	}
}

func TestStopClosesTheListener(t *testing.T) {
	port := freePort(t)
	agent, _ := startApp(t, configFor(port, "first-token"))
	url := agent.baseURL() + "/health"

	if err := agent.Stop(); err != nil {
		t.Fatalf("Stop returned an error: %v", err)
	}

	if got := get(t, url, ""); got != 0 {
		t.Errorf("the agent is still serving after Stop, got status %d", got)
	}
}

func TestStopIsSafeToCallTwice(t *testing.T) {
	port := freePort(t)
	agent, _ := startApp(t, configFor(port, "first-token"))

	if err := agent.Stop(); err != nil {
		t.Fatalf("first Stop returned an error: %v", err)
	}
	if err := agent.Stop(); err != nil {
		t.Errorf("second Stop returned an error: %v", err)
	}
}

func TestReloadAppliesANewToken(t *testing.T) {
	port := freePort(t)
	agent, path := startApp(t, configFor(port, "first-token"))

	writeConfig(t, path, configFor(port, "second-token"))
	if err := agent.Reload(); err != nil {
		t.Fatalf("Reload returned an error: %v", err)
	}

	if got := get(t, agent.baseURL()+"/", "first-token"); got != http.StatusUnauthorized {
		t.Errorf("old token returned %d, want 401 after reload", got)
	}
	if got := get(t, agent.baseURL()+"/", "second-token"); got != http.StatusOK {
		t.Errorf("new token returned %d, want 200 after reload", got)
	}
}

func TestReloadWithAnInvalidConfigKeepsServing(t *testing.T) {
	// The behaviour that matters. Someone mistypes a key in a running agent's
	// config: the agent must say so and carry on with what it already had,
	// rather than exiting and taking the panel dark.
	port := freePort(t)
	agent, path := startApp(t, configFor(port, "first-token"))

	writeConfig(t, path, `{"token": "", "sources": {}}`)
	err := agent.Reload()

	if err == nil {
		t.Fatal("Reload should refuse an invalid config")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error should name the offending key, got: %v", err)
	}
	if got := get(t, agent.baseURL()+"/health", ""); got != http.StatusOK {
		t.Errorf("GET /health = %d, want the previous config still serving", got)
	}
	if got := get(t, agent.baseURL()+"/", "first-token"); got != http.StatusOK {
		t.Errorf("the previous token stopped working after a failed reload, got %d", got)
	}
}

func TestReloadRejectsAnUnknownSourceAndKeepsServing(t *testing.T) {
	port := freePort(t)
	agent, path := startApp(t, configFor(port, "first-token"))

	writeConfig(t, path, fmt.Sprintf(`{
		"listen": "127.0.0.1:%d",
		"token": "first-token",
		"sources": {"spotify": {"enabled": true, "interval_ms": 1000}}
	}`, port))
	err := agent.Reload()

	if err == nil {
		t.Fatal("Reload should refuse a source with no implementation")
	}
	if got := get(t, agent.baseURL()+"/health", ""); got != http.StatusOK {
		t.Errorf("GET /health = %d, want the previous config still serving", got)
	}
}

func TestReloadBeforeStartIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeConfig(t, path, configFor(freePort(t), "a-token"))
	agent := New(path, "test-version", discardLogger())

	if err := agent.Reload(); err == nil {
		t.Error("Reload should report that there is nothing running to reload")
	}
}

func TestOpenURLTargetsLoopbackAndCarriesTheToken(t *testing.T) {
	// The listen address is a wildcard in the shipped config, and a browser
	// cannot open 0.0.0.0. The token has to be there too or the page 401s.
	port := freePort(t)
	path := filepath.Join(t.TempDir(), "config.json")
	writeConfig(t, path, fmt.Sprintf(`{
		"listen": "0.0.0.0:%d",
		"token": "a-token",
		"sources": {"clock": {"enabled": true, "interval_ms": 1000}}
	}`, port))

	agent := New(path, "test-version", discardLogger())
	if err := agent.Start(); err != nil {
		t.Fatalf("Start returned an error: %v", err)
	}
	defer agent.Stop()

	url := agent.OpenURL()
	if strings.Contains(url, "0.0.0.0") {
		t.Errorf("OpenURL = %q, a browser cannot open a wildcard address", url)
	}
	if !strings.Contains(url, "127.0.0.1") {
		t.Errorf("OpenURL = %q, want loopback", url)
	}
	if !strings.Contains(url, "token=a-token") {
		t.Errorf("OpenURL = %q, want the token so the page authenticates", url)
	}
}

func TestSourcesRunAfterStart(t *testing.T) {
	port := freePort(t)
	agent, _ := startApp(t, configFor(port, "first-token"))

	// The clock source cannot fail, so a status of ok proves the runner is
	// wired up and polling rather than merely constructed.
	deadline := 200
	for i := 0; i < deadline; i++ {
		if strings.Contains(healthBody(t, agent.baseURL()), `"status":"ok"`) {
			return
		}
	}
	t.Error("no source reported ok, so the runner is not polling")
}

func healthBody(t *testing.T, base string) string {
	t.Helper()
	resp, err := http.Get(base + "/health")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

func postJSON(t *testing.T, url, token string, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, ""
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(respBody)
}

func TestSettingsGetReportsTheConfiguredValues(t *testing.T) {
	port := freePort(t)
	agent, _ := startApp(t, configFor(port, "first-token"))

	req, _ := http.NewRequest(http.MethodGet, agent.baseURL()+"/settings", nil)
	req.Header.Set("Authorization", "Bearer first-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /settings: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, respBody)
	}
	var decoded struct {
		AccentColor string `json:"accent_color"`
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		t.Fatalf("decoding response: %v\nbody: %s", err, respBody)
	}
	if decoded.AccentColor != "" {
		t.Errorf("AccentColor = %q, want empty for a config with none set", decoded.AccentColor)
	}
}

func TestSettingsRequiresTheToken(t *testing.T) {
	port := freePort(t)
	agent, _ := startApp(t, configFor(port, "first-token"))

	if got := get(t, agent.baseURL()+"/settings", ""); got != http.StatusUnauthorized {
		t.Errorf("GET /settings with no token = %d, want 401", got)
	}
}

func TestSettingsPostSavesAndReloads(t *testing.T) {
	port := freePort(t)
	agent, path := startApp(t, configFor(port, "first-token"))

	status, respBody := postJSON(t, agent.baseURL()+"/settings", "first-token", `{"accent_color":"#7c83fd"}`)
	if status != http.StatusOK {
		t.Fatalf("POST /settings = %d, want 200: %s", status, respBody)
	}

	// Written to config.json...
	saved, err := config.Load(path)
	if err != nil {
		t.Fatalf("reloading config.json from disk: %v", err)
	}
	if saved.AccentColor != "#7c83fd" {
		t.Errorf("config.json accent_color = %q, want %q", saved.AccentColor, "#7c83fd")
	}

	// ...and applied live, without restarting the process. The reload that
	// picks it up is scheduled slightly after the response above, so this
	// polls briefly rather than assuming it has already happened.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if strings.Contains(healthBody(t, agent.baseURL()), `"accent_color":"#7c83fd"`) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("/health never reported the new accent_color after saving")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSettingsPostRejectsAnInvalidColour(t *testing.T) {
	port := freePort(t)
	agent, path := startApp(t, configFor(port, "first-token"))

	status, _ := postJSON(t, agent.baseURL()+"/settings", "first-token", `{"accent_color":"not-a-colour"}`)
	if status != http.StatusBadRequest {
		t.Errorf("POST with an invalid colour = %d, want 400", status)
	}

	saved, err := config.Load(path)
	if err != nil {
		t.Fatalf("reloading config.json: %v", err)
	}
	if saved.AccentColor != "" {
		t.Errorf("config.json accent_color = %q, want unchanged (empty) after a rejected save", saved.AccentColor)
	}
}

func TestSettingsPostSavesHiddenFaces(t *testing.T) {
	port := freePort(t)
	agent, path := startApp(t, configFor(port, "first-token"))

	status, respBody := postJSON(t, agent.baseURL()+"/settings", "first-token", `{"hidden_faces":["clock","telemetry"]}`)
	if status != http.StatusOK {
		t.Fatalf("POST /settings = %d, want 200: %s", status, respBody)
	}

	saved, err := config.Load(path)
	if err != nil {
		t.Fatalf("reloading config.json: %v", err)
	}
	if len(saved.HiddenFaces) != 2 || saved.HiddenFaces[0] != "clock" || saved.HiddenFaces[1] != "telemetry" {
		t.Errorf("config.json hidden_faces = %v, want [clock telemetry]", saved.HiddenFaces)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if strings.Contains(healthBody(t, agent.baseURL()), `"hidden_faces":["clock","telemetry"]`) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("/health never reported the new hidden_faces after saving")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSettingsPostSavesClockStyle(t *testing.T) {
	port := freePort(t)
	agent, path := startApp(t, configFor(port, "first-token"))

	status, respBody := postJSON(t, agent.baseURL()+"/settings", "first-token", `{"clock_style":"analogue"}`)
	if status != http.StatusOK {
		t.Fatalf("POST /settings = %d, want 200: %s", status, respBody)
	}

	saved, err := config.Load(path)
	if err != nil {
		t.Fatalf("reloading config.json: %v", err)
	}
	if saved.ClockStyle != "analogue" {
		t.Errorf("config.json clock_style = %q, want %q", saved.ClockStyle, "analogue")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if strings.Contains(healthBody(t, agent.baseURL()), `"clock_style":"analogue"`) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("/health never reported the new clock_style after saving")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSettingsPostRejectsAnUnknownClockStyle(t *testing.T) {
	port := freePort(t)
	agent, _ := startApp(t, configFor(port, "first-token"))

	status, respBody := postJSON(t, agent.baseURL()+"/settings", "first-token", `{"clock_style":"roman-numerals"}`)
	if status != http.StatusBadRequest {
		t.Errorf("POST /settings with an unknown clock_style = %d, want 400: %s", status, respBody)
	}
}

func TestSettingsPostRejectsHidingEveryFace(t *testing.T) {
	port := freePort(t)
	agent, path := startApp(t, configFor(port, "first-token"))

	all := `["` + strings.Join(config.KnownFaces, `","`) + `"]`
	status, _ := postJSON(t, agent.baseURL()+"/settings", "first-token", `{"hidden_faces":`+all+`}`)
	if status != http.StatusBadRequest {
		t.Errorf("POST hiding every face = %d, want 400", status)
	}

	saved, err := config.Load(path)
	if err != nil {
		t.Fatalf("reloading config.json: %v", err)
	}
	if len(saved.HiddenFaces) != 0 {
		t.Errorf("config.json hidden_faces = %v, want unchanged (empty) after a rejected save", saved.HiddenFaces)
	}
}
