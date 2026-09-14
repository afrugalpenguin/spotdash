package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/afrugalpenguin/spotdash/agent/internal/state"
)

const (
	// protocolName is echoed back to the client so the browser accepts the
	// connection.
	protocolName = "spotdash.v1"
	// tokenProtocolPrefix carries the bearer token. A browser cannot set
	// headers on a WebSocket, and a query parameter would put the secret
	// somewhere that gets logged.
	tokenProtocolPrefix = "bearer."
	// writeTimeout bounds a single write so one wedged client cannot hold a
	// goroutine open indefinitely.
	writeTimeout = 10 * time.Second
	// keepaliveInterval is how often a silent connection is pinged.
	keepaliveInterval = 30 * time.Second
)

// Message is one frame on the wire.
type Message struct {
	Source string `json:"source"`
	TS     string `json:"ts"`
	Data   any    `json:"data"`
}

// HandleWebSocket mounts /ws.
//
// It is mounted outside the bearer token middleware and authorises itself,
// because a browser cannot set headers on a WebSocket and the token therefore
// arrives as a subprotocol value that the generic check cannot read.
func (s *Server) HandleWebSocket() {
	s.socket = http.HandlerFunc(s.handleWebSocket)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// The generic middleware cannot see the token here: it travels as a
	// subprotocol. Authorise before the upgrade completes so an unauthorised
	// client never reaches a live socket.
	if !s.socketAuthorised(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="spotdash"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{protocolName},
		// Same origin only. The panel is served from this origin, so nothing
		// legitimate connects from anywhere else.
		OriginPatterns: nil,
	})
	if err != nil {
		s.log.Debug("websocket upgrade failed", "error", err)
		return
	}
	defer conn.CloseNow()

	// The panel only listens, so nothing here reads application messages. A
	// connection that is never read from also never processes control frames,
	// which means a client's close is not acknowledged until it times out and
	// a client that has vanished is never noticed. CloseRead drains and
	// discards incoming frames, and cancels this context when the peer goes
	// away.
	ctx := conn.CloseRead(r.Context())

	// Subscribe before snapshotting, so an update that lands between the two
	// is queued rather than lost.
	events, cancel := s.opts.Store.Subscribe()
	defer cancel()

	if err := s.sendSnapshot(ctx, conn); err != nil {
		s.log.Debug("websocket snapshot failed", "error", err)
		return
	}

	s.log.Debug("websocket client connected", "remote", r.RemoteAddr)
	defer s.log.Debug("websocket client disconnected", "remote", r.RemoteAddr)

	// A kiosk panel on wifi can disappear without closing anything. Without a
	// ping there is nothing to notice that with, because a clock source that
	// writes every second would otherwise be the only liveness signal and a
	// write to a dead socket can sit buffered for a long time.
	keepalive := time.NewTicker(keepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "server shutting down")
			return
		case <-keepalive.C:
			pingCtx, cancelPing := context.WithTimeout(ctx, writeTimeout)
			err := conn.Ping(pingCtx)
			cancelPing()
			if err != nil {
				s.log.Debug("websocket ping failed", "remote", r.RemoteAddr, "error", err)
				return
			}
		case entry, open := <-events:
			if !open {
				// The store dropped this subscriber for falling behind. Close
				// so the client reconnects and gets a fresh snapshot rather
				// than continuing with gaps it cannot detect.
				conn.Close(websocket.StatusTryAgainLater, "client fell behind")
				return
			}
			if err := s.send(ctx, conn, entry); err != nil {
				s.log.Debug("websocket write failed", "error", err)
				return
			}
		}
	}
}

// sendSnapshot writes the current reading for every source that has one, using
// the same message shape as a live update so the client has one code path.
func (s *Server) sendSnapshot(ctx context.Context, conn *websocket.Conn) error {
	for _, entry := range s.opts.Store.Snapshot() {
		if entry.UpdatedAt.IsZero() {
			// Nothing has been polled yet. An empty message would have the
			// panel render a blank value as though it were a reading.
			continue
		}
		if err := s.send(ctx, conn, entry); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) send(ctx context.Context, conn *websocket.Conn, entry state.Entry) error {
	payload, err := json.Marshal(Message{
		Source: entry.Source,
		TS:     entry.UpdatedAt.UTC().Format(time.RFC3339),
		Data:   entry.Data,
	})
	if err != nil {
		return err
	}

	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, payload)
}

// socketAuthorised accepts the token as a subprotocol value, as a session
// cookie, or as a bearer header, in that order.
func (s *Server) socketAuthorised(r *http.Request) bool {
	for _, protocol := range requestedProtocols(r) {
		if strings.HasPrefix(protocol, tokenProtocolPrefix) &&
			s.secretEquals(strings.TrimPrefix(protocol, tokenProtocolPrefix)) {
			return true
		}
	}
	if cookie, err := r.Cookie(sessionCookieName); err == nil && s.secretEquals(cookie.Value) {
		return true
	}
	return s.tokenValid(r.Header.Get("Authorization"))
}

func requestedProtocols(r *http.Request) []string {
	var out []string
	for _, header := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, value := range strings.Split(header, ",") {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}
