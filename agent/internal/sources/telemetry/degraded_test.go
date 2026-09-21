package telemetry_test

// The degraded NVML path through the whole agent, with only the GPU reader
// stubbed. The reading must still carry CPU, RAM and disks, the source must
// report degraded with the reason, and gpu must be null.

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
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	var body healthBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /health: %v", err)
	}
	return body
}

func TestAgentKeepsRunningWhenNVMLIsUnavailable(t *testing.T) {
	src := telemetry.NewWithUnavailableGPU(
		50*time.Millisecond,
		errors.New("nvml unavailable: library not found"),
	)
	httpSrv := startAgent(t, src)

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
		t.Fatal("telemetry never published a reading")
	}
	if entry.Status != "degraded" {
		t.Errorf("status = %q, want degraded", entry.Status)
	}
	if !strings.Contains(entry.LastError, "nvml") {
		t.Errorf("last_error = %q, want the NVML reason", entry.LastError)
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
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	// The first frame is the protocol hello. The reading follows.
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("Read hello: %v", err)
	}
	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
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
		t.Fatalf("Unmarshal %s: %v", raw, err)
	}

	if message.Source != "telemetry" {
		t.Fatalf("source = %q, want telemetry", message.Source)
	}
	if message.Data.GPU != nil {
		t.Error("gpu is set, want null")
	}
	if !strings.Contains(string(raw), `"gpu":null`) {
		t.Errorf("gpu not null in:\n%s", raw)
	}

	if len(message.Data.CPU.PerCore) == 0 {
		t.Error("per core CPU missing")
	}
	if message.Data.RAM.TotalBytes == 0 {
		t.Error("RAM missing")
	}
	if len(message.Data.Disks) == 0 {
		t.Error("no disks reported")
	}
	for _, d := range message.Data.Disks {
		if d.Mount == "" || d.TotalBytes == 0 {
			t.Errorf("incomplete disk: %+v", d)
		}
	}
}
