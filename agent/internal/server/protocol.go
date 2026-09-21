package server

import (
	"regexp"
	"strconv"
)

// ProtocolVersion is what the agent and the shell must agree on: the bridge
// methods and the provisioning payload. Bump it only when one of those
// changes incompatibly. See docs/architecture.md, "Protocol version".
const ProtocolVersion = 1

// protocolSource is the source name of the hello frame. It never enters the
// store, so it is not in /health or in a snapshot.
const protocolSource = "protocol"

// shellTokenPattern matches the token the shell appends to the WebView user
// agent, for example "spotdash-shell/0.1.0 proto/1".
var shellTokenPattern = regexp.MustCompile(`(?:^|\s)spotdash-shell/(\S+) proto/(\d+)(?:\s|$)`)

// shellToken reads the shell's version and protocol from a User-Agent. ok is
// false for a browser or any agent string without a valid token.
func shellToken(userAgent string) (version string, protocol int, ok bool) {
	match := shellTokenPattern.FindStringSubmatch(userAgent)
	if match == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(match[2])
	if err != nil {
		return "", 0, false
	}
	return match[1], n, true
}
