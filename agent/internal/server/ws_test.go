package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/afrugalpenguin/spotdash/agent/internal/state"
)

// liveServer starts a real HTTP server with /ws mounted. A WebSocket upgrade
// cannot go through httptest.ResponseRecorder.
func liveServer(t *testing.T) (*httptest.Server, *state.Store) {
	t.Helper()
	return liveServerWith(t, Options{})
}

// liveServerWith is liveServer with extra options. The token, version, start
// time and store are filled in when left empty.
func liveServerWith(t *testing.T, opts Options) (*httptest.Server, *state.Store) {
	t.Helper()
	if opts.Store == nil {
		opts.Store = state.New()
	}
	store := opts.Store
	opts.Token = testToken
	opts.Version = "test-version"
	opts.Started = time.Now()
	srv := New(opts)
	srv.HandleWebSocket()

	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)
	return httpSrv, store
}

func wsURL(httpSrv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"
}

// dialRaw opens a socket carrying the token the way the panel does, with
// extra request headers, and leaves the hello frame unread.
func dialRaw(t *testing.T, httpSrv *httptest.Server, token string, header http.Header) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(httpSrv), &websocket.DialOptions{
		Subprotocols: []string{"spotdash.v1", "bearer." + token},
		HTTPHeader:   header,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "test over") })
	return conn
}

// dial opens a socket and reads past the hello frame, so a test starts at the
// first reading.
func dial(t *testing.T, httpSrv *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	conn := dialRaw(t, httpSrv, token, nil)
	if hello := readMessage(t, conn); hello.Source != "protocol" {
		t.Fatalf("first frame came from %q, want protocol", hello.Source)
	}
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

func TestProtocolHelloIsTheFirstFrame(t *testing.T) {
	httpSrv, store := liveServer(t)
	store.Register("clock", state.StatusDegraded, "")
	store.Update("clock", map[string]any{"time": "10:00"})

	conn := dialRaw(t, httpSrv, testToken, nil)

	hello := readMessage(t, conn)
	if hello.Source != "protocol" {
		t.Fatalf("first frame came from %q, want protocol", hello.Source)
	}
	if _, err := time.Parse(time.RFC3339, hello.TS); err != nil {
		t.Errorf("ts = %q, want RFC3339: %v", hello.TS, err)
	}
	var data struct {
		Protocol int    `json:"protocol"`
		Agent    string `json:"agent"`
	}
	if err := json.Unmarshal(hello.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.Protocol != ProtocolVersion {
		t.Errorf("protocol = %d, want %d", data.Protocol, ProtocolVersion)
	}
	if data.Agent != "test-version" {
		t.Errorf("agent = %q, want test-version", data.Agent)
	}
	if got := readMessage(t, conn).Source; got != "clock" {
		t.Errorf("second frame came from %q, want clock", got)
	}
}

// syncBuffer lets the test read what the server goroutine logged.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// connectWithAgent dials with the given User-Agent and returns what the
// server logged once the hello frame has arrived. The warning is written
// before the hello is sent.
func connectWithAgent(t *testing.T, userAgent string) string {
	t.Helper()
	logs := &syncBuffer{}
	httpSrv, _ := liveServerWith(t, Options{Logger: slog.New(slog.NewTextHandler(logs, nil))})

	header := http.Header{}
	if userAgent != "" {
		header.Set("User-Agent", userAgent)
	}
	conn := dialRaw(t, httpSrv, testToken, header)
	readMessage(t, conn)
	return logs.String()
}

func TestProtocolWarnsOnceWhenTheShellDiffers(t *testing.T) {
	out := connectWithAgent(t, "Mozilla/5.0 (Linux; Android 11) spotdash-shell/0.3.0 proto/7")

	if got := strings.Count(out, "level=WARN"); got != 1 {
		t.Fatalf("got %d warnings, want 1\nlog: %s", got, out)
	}
	for _, want := range []string{"shell_protocol=7", "agent_protocol=1", "shell_version=0.3.0", "agent_version=test-version"} {
		if !strings.Contains(out, want) {
			t.Errorf("log does not contain %q\nlog: %s", want, out)
		}
	}
}

func TestProtocolStaysQuietWhenTheShellMatches(t *testing.T) {
	out := connectWithAgent(t, "Mozilla/5.0 spotdash-shell/0.1.0 proto/1")

	if strings.Contains(out, "level=WARN") {
		t.Errorf("unexpected warning\nlog: %s", out)
	}
}

func TestProtocolStaysQuietForABrowser(t *testing.T) {
	out := connectWithAgent(t, "Mozilla/5.0 (Windows NT 10.0) Chrome/120")

	if strings.Contains(out, "level=WARN") {
		t.Errorf("unexpected warning\nlog: %s", out)
	}
}

func TestProtocolReadsTheShellToken(t *testing.T) {
	tests := []struct {
		userAgent string
		version   string
		protocol  int
		ok        bool
	}{
		{"Mozilla/5.0 spotdash-shell/0.1.0 proto/1", "0.1.0", 1, true},
		{"spotdash-shell/0.1.0-rc1 proto/12", "0.1.0-rc1", 12, true},
		{"", "", 0, false},
		{"Mozilla/5.0 Chrome/120", "", 0, false},
		{"spotdash-shell/0.1.0", "", 0, false},
		{"spotdash-shell/0.1.0 proto/", "", 0, false},
		{"spotdash-shell/0.1.0 proto/x", "", 0, false},
		{"xspotdash-shell/0.1.0 proto/1", "", 0, false},
		{"spotdash-shell/0.1.0 proto/99999999999999999999", "", 0, false},
	}
	for _, tt := range tests {
		version, protocol, ok := shellToken(tt.userAgent)
		if version != tt.version || protocol != tt.protocol || ok != tt.ok {
			t.Errorf("shellToken(%q) = %q, %d, %v, want %q, %d, %v",
				tt.userAgent, version, protocol, ok, tt.version, tt.protocol, tt.ok)
		}
	}
}
