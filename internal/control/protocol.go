package control

// DefaultSocketPath is the default Unix socket path for the control server.
const DefaultSocketPath = "/var/run/sluice/control.sock"

// Request represents a control command sent from the CLI to the running server.
type Request struct {
	Action string `json:"action"` // "deny", "allow", "remove", "rules"
	Domain string `json:"domain,omitempty"`
}

// Response represents the server's reply to a control command.
type Response struct {
	OK      bool        `json:"ok"`
	Message string      `json:"message,omitempty"`
	Error   string      `json:"error,omitempty"`
	Rules   []RuleEntry `json:"rules,omitempty"`
}

// RuleEntry represents a single domain rule in the rules listing.
type RuleEntry struct {
	Domain string `json:"domain"`
	Action string `json:"action"` // "allow" or "deny"
	Source string `json:"source"` // "config" or "runtime"
}
