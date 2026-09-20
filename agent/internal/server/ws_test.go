package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/afrugalpenguin/spotdash/agent/internal/state"
)

// liveServer starts a real HTTP server with /ws mounted. A WebSocket upgrade
// cannot go through httptest.ResponseRecorder.
func liveServer(t *testing.T) (*httptest.Server, *state.Store) {
	t.Helper()
	store := state.New()
	srv := New(Options{
		Token:   testToken,
		Version: "test-version",
		Started: time.Now(),
		Store:   store,
	})
	srv.HandleWebSocket()

	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)
	return httpSrv, store
}

func wsURL(httpSrv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"
}

// dial opens a socket carrying the token the way the panel does.
func dial(t *testing.T, httpSrv *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv), &websocket.DialOptions{
		Subprotocols: []string{"spotdash.v1", "bearer." + token},
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "test over") })
	return conn
}

type wsMessage struct {
	Source string          `json:"source"`
	TS     string          `json:"ts"`
	Data   json.RawMessage `json:"data"`
}

func readMessage(t *testing.T, conn *websocket.Conn) wsMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var msg wsMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return msg
}

func TestSocketRejectsAMissingToken(t *testing.T) {
	httpSrv, _ := liveServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL(httpSrv), nil)
	if err == nil {
		conn.Close(websocket.StatusNormalClosure, "")
		t.Fatal("Dial with no token succeeded, want error")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestSocketRejectsAWrongToken(t *testing.T) {
	httpSrv, _ := liveServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv), &websocket.DialOptions{
		Subprotocols: []string{"spotdash.v1", "bearer.not-the-token"},
	})
	if err == nil {
		conn.Close(websocket.StatusNormalClosure, "")
		t.Fatal("Dial with a wrong token succeeded, want error")
	}
}

func TestSocketAcceptsTheSessionCookie(t *testing.T) {
	// The page already holds a session cookie, so it has to work here too.
	httpSrv, _ := liveServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	header := http.Header{}
	header.Set("Cookie", sessionCookieName+"="+testToken)
	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv), &websocket.DialOptions{
		HTTPHeader:   header,
		Subprotocols: []string{"spotdash.v1"},
	})
	if err != nil {
		t.Fatalf("Dial with a session cookie: %v", err)
	}
	conn.Close(websocket.StatusNormalClosure, "")
}

func TestSocketSendsTheFullStateOnConnect(t *testing.T) {
	httpSrv, store := liveServer(t)
	store.Register("clock", state.StatusDegraded, "")
	store.Register("telemetry", state.StatusDegraded, "")
	store.Update("clock", map[string]any{"time": "10:00"})
	store.Update("telemetry", map[string]any{"cpu": 12})

	conn := dial(t, httpSrv, testToken)

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		msg := readMessage(t, conn)
		seen[msg.Source] = true
		if msg.TS == "" {
			t.Errorf("message for %q has no timestamp", msg.Source)
		}
	}

	if !seen["clock"] || !seen["telemetry"] {
		t.Errorf("snapshot covered %v, want both sources", seen)
	}
}

func TestSnapshotSkipsSourcesThatHaveNotPolled(t *testing.T) {
	// A source with no reading sends nothing.
	httpSrv, store := liveServer(t)
	store.Register("clock", state.StatusDegraded, "awaiting first poll")
	store.Register("telemetry", state.StatusDisabled, "")
	store.Update("clock", map[string]any{"time": "10:00"})

	conn := dial(t, httpSrv, testToken)

	first := readMessage(t, conn)
	if first.Source != "clock" {
		t.Fatalf("first message came from %q, want clock", first.Source)
	}

	// Nothing else should arrive on its own.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, raw, err := conn.Read(ctx); err == nil {
		t.Errorf("unexpected extra message: %s", raw)
	}
}

func TestSocketPushesLiveUpdates(t *testing.T) {
	httpSrv, store := liveServer(t)
	store.Register("clock", state.StatusDegraded, "")
	store.Update("clock", map[string]any{"time": "10:00"})

	conn := dial(t, httpSrv, testToken)
	if got := readMessage(t, conn).Source; got != "clock" {
		t.Fatalf("snapshot came from %q, want clock", got)
	}

	store.Update("clock", map[string]any{"time": "10:01"})

	msg := readMessage(t, conn)
	if msg.Source != "clock" {
		t.Errorf("Source = %q, want clock", msg.Source)
	}
	var data struct {
		Time string `json:"time"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.Time != "10:01" {
		t.Errorf("time = %q, want 10:01", data.Time)
	}
}

func TestMessageTimestampIsRFC3339(t *testing.T) {
	httpSrv, store := liveServer(t)
	store.Register("clock", state.StatusDegraded, "")
	store.Update("clock", map[string]any{"time": "10:00"})

	conn := dial(t, httpSrv, testToken)

	msg := readMessage(t, conn)
	if _, err := time.Parse(time.RFC3339, msg.TS); err != nil {
		t.Errorf("ts = %q, want RFC3339: %v", msg.TS, err)
	}
}

func TestTwoClientsBothReceiveUpdates(t *testing.T) {
	httpSrv, store := liveServer(t)
	store.Register("clock", state.StatusDegraded, "")
	store.Update("clock", map[string]any{"time": "10:00"})

	first := dial(t, httpSrv, testToken)
	second := dial(t, httpSrv, testToken)
	readMessage(t, first)
	readMessage(t, second)

	store.Update("clock", map[string]any{"time": "10:02"})

	if got := readMessage(t, first).Source; got != "clock" {
		t.Errorf("first client got %q", got)
	}
	if got := readMessage(t, second).Source; got != "clock" {
		t.Errorf("second client got %q", got)
	}
}

func TestSocketNegotiatesTheProtocol(t *testing.T) {
	// The server must echo a protocol or the browser refuses the connection.
	httpSrv, _ := liveServer(t)

	conn := dial(t, httpSrv, testToken)

	if got := conn.Subprotocol(); got != "spotdash.v1" {
		t.Errorf("negotiated subprotocol = %q, want spotdash.v1", got)
	}
}
