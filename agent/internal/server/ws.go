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
	// protocolName is echoed back so the browser accepts the connection.
	protocolName = "spotdash.v1"
	// tokenProtocolPrefix carries the bearer token. A browser cannot set
	// headers on a WebSocket, and a query parameter would get logged.
	tokenProtocolPrefix = "bearer."
	// writeTimeout stops a wedged client holding a goroutine open.
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

// HandleWebSocket mounts /ws outside the token middleware. It authorises itself
// because the token arrives as a subprotocol.
func (s *Server) HandleWebSocket() {
	s.socket = http.HandlerFunc(s.handleWebSocket)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Authorise before the upgrade so an unauthorised client never gets a socket.
	if !s.socketAuthorised(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="spotdash"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{protocolName},
		// Same origin only.
		OriginPatterns: nil,
	})
	if err != nil {
		s.log.Debug("websocket upgrade failed", "error", err)
		return
	}
	defer conn.CloseNow()

	// Nothing reads application messages. CloseRead still drains control frames
	// and cancels ctx when the peer goes. See docs/architecture.md, "Liveness".
	ctx := conn.CloseRead(r.Context())

	// Subscribe before the snapshot so an update in between is queued.
	events, cancel := s.opts.Store.Subscribe()
	defer cancel()

	s.checkShellProtocol(r)
	if err := s.sendHello(ctx, conn); err != nil {
		s.log.Debug("websocket hello failed", "error", err)
		return
	}
	if err := s.sendSnapshot(ctx, conn); err != nil {
		s.log.Debug("websocket snapshot failed", "error", err)
		return
	}

	s.log.Debug("websocket client connected", "remote", r.RemoteAddr)
	defer s.log.Debug("websocket client disconnected", "remote", r.RemoteAddr)

	// A wifi kiosk can vanish without closing. Ping to notice.
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
				// The store dropped this subscriber for falling behind. Closing
				// makes the client reconnect for a fresh snapshot.
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

// sendHello writes the first frame on every connection: the protocol version
// and the agent version.
func (s *Server) sendHello(ctx context.Context, conn *websocket.Conn) error {
	payload, err := json.Marshal(Message{
		Source: protocolSource,
		TS:     s.opts.Now().UTC().Format(time.RFC3339),
		Data: map[string]any{
			"protocol": ProtocolVersion,
			"agent":    s.opts.Version,
		},
	})
	if err != nil {
		return err
	}

	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, payload)
}

// checkShellProtocol logs one warning when the shell's user agent names a
// different protocol. A browser has no token and is not checked.
func (s *Server) checkShellProtocol(r *http.Request) {
	version, protocol, ok := shellToken(r.UserAgent())
	if !ok || protocol == ProtocolVersion {
		return
	}
	s.log.Warn("shell and agent protocol versions differ",
		"shell_version", version, "shell_protocol", protocol,
		"agent_version", s.opts.Version, "agent_protocol", ProtocolVersion)
}

// sendSnapshot writes the current reading of every source that has one, in the
// live message shape.
func (s *Server) sendSnapshot(ctx context.Context, conn *websocket.Conn) error {
	for _, entry := range s.opts.Store.Snapshot() {
		if entry.UpdatedAt.IsZero() {
			// Never polled. An empty message would render as a blank reading.
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
