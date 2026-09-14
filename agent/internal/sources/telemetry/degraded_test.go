package telemetry_test

// The degraded NVML path, exercised through the whole agent: the real registry
// runner, the real state store, the real HTTP server, and a real WebSocket
// client. Only the GPU reader is stubbed, because the alternative is renaming a
// DLL in the Windows system directory.
//
// What must hold is that a machine which cannot read its GPU still publishes a
// complete CPU, RAM and disk reading, reports itself degraded with the reason
// attached, and sends a null gpu rather than an empty object that would render
// as real zeroes.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/afrugalpenguin/spotdash/agent/internal/server"
	"github.com/afrugalpenguin/spotdash/agent/internal/sources"
	"github.com/afrugalpenguin/spotdash/agent/internal/sources/telemetry"
	"github.com/afrugalpenguin/spotdash/agent/internal/state"
)

const degradedToken = "degraded-path-token"

func startAgent(t *testing.T, src sources.Source) *httptest.Server {
	t.Helper()

	store := state.New()
	store.Register(src.Name(), state.StatusDegraded, "awaiting first poll")

	runner := sources.NewRunner(store, nil)
	ctx, cancel := context.WithCancel(context.Background())
	runner.Start(ctx, []sources.Source{src})
	t.Cleanup(func() {
		cancel()
		runner.Wait()
	})

	srv := server.New(server.Options{
		Token:   degradedToken,
		Version: "degraded-test",
		Started: time.Now(),
		Store:   store,
	})
	srv.HandleWebSocket()

	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)
	return httpSrv
}

type healthBody struct {
	Sources map[string]struct {
		Status     string `json:"status"`
		LastUpdate string `json:"last_update"`
		LastError  string `json:"last_error"`
	} `json:"sources"`
}

func readHealth(t *testing.T, httpSrv *httptest.Server) healthBody {
	t.Helper()
	resp, err := http.Get(httpSrv.URL + "/health")
	if err != nil {
		t.Fatalf("reading /health: %v", err)
	}
	defer resp.Body.Close()

	var body healthBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding /health: %v", err)
	}
	return body
}

func TestAgentKeepsRunningWhenNVMLIsUnavailable(t *testing.T) {
	src := telemetry.NewWithUnavailableGPU(
		50*time.Millisecond,
		errors.New("nvml unavailable: library not found"),
	)
	httpSrv := startAgent(t, src)

	// /health should settle on degraded with the reason attached.
	deadline := time.Now().Add(5 * time.Second)
	var reported healthBody
	for time.Now().Before(deadline) {
		reported = readHealth(t, httpSrv)
		if reported.Sources["telemetry"].LastUpdate != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	entry := reported.Sources["telemetry"]
	if entry.LastUpdate == "" {
		t.Fatal("telemetry never published a reading, so the agent did not survive a missing GPU")
	}
	if entry.Status != "degraded" {
		t.Errorf("status = %q, want degraded", entry.Status)
	}
	if !strings.Contains(entry.LastError, "nvml") {
		t.Errorf("last_error = %q, want the NVML reason recorded", entry.LastError)
	}
}

func TestDegradedReadingStillCarriesTheMachineOverTheSocket(t *testing.T) {
	src := telemetry.NewWithUnavailableGPU(
		50*time.Millisecond,
		errors.New("nvml unavailable: library not found"),
	)
	httpSrv := startAgent(t, src)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpSrv.URL, "http")+"/ws", &websocket.DialOptions{
		Subprotocols: []string{"spotdash.v1", "bearer." + degradedToken},
	})
	if err != nil {
		t.Fatalf("dialling the socket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("reading from the socket: %v", err)
	}

	var message struct {
		Source string `json:"source"`
		Data   struct {
			CPU struct {
				Percent float64   `json:"percent"`
				PerCore []float64 `json:"per_core"`
			} `json:"cpu"`
			RAM struct {
				TotalBytes uint64 `json:"total_bytes"`
			} `json:"ram"`
			Disks []struct {
				Mount      string `json:"mount"`
				TotalBytes uint64 `json:"total_bytes"`
			} `json:"disks"`
			GPU *struct{} `json:"gpu"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &message); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}

	if message.Source != "telemetry" {
		t.Fatalf("source = %q, want telemetry", message.Source)
	}
	if message.Data.GPU != nil {
		t.Error("gpu should be null when NVML is unavailable")
	}
	if !strings.Contains(string(raw), `"gpu":null`) {
		t.Errorf("gpu should serialise as null, not an empty object:\n%s", raw)
	}

	// The point of degrading rather than failing: everything else is still here
	// and worth looking at.
	if len(message.Data.CPU.PerCore) == 0 {
		t.Error("per core CPU should still be reported on a machine with no GPU")
	}
	if message.Data.RAM.TotalBytes == 0 {
		t.Error("RAM should still be reported on a machine with no GPU")
	}
	if len(message.Data.Disks) == 0 {
		t.Error("at least one disk should still be reported on a machine with no GPU")
	}
	for _, d := range message.Data.Disks {
		if d.Mount == "" || d.TotalBytes == 0 {
			t.Errorf("disk entry is incomplete: %+v", d)
		}
	}
}
