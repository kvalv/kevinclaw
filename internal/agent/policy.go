package agent

// Restrictions returned by a ToolPolicy.
type Restrictions struct {
	DisallowedServers []string // MCP server names to block
	AllowedTools      []string // if set, overrides default tool access
}

// ToolPolicy decides tool restrictions for a given message context.
type ToolPolicy func(userID, channel string) Restrictions

// ReadOnlyTools is the set of tools available to non-owners.
var ReadOnlyTools = []string{"Read", "Glob", "Grep", "WebSearch", "WebFetch"}

// PrivateServers is the list of MCP servers blocked for non-owners.
var PrivateServers = []string{"gcal", "linear", "cron", "debug", "homeassistant"}

// NewOwnerPolicy creates a policy where the owner gets full access
// and everyone else is restricted to read-only tools with no private MCP servers.
func NewOwnerPolicy(ownerID string) ToolPolicy {
	return func(userID, channel string) Restrictions {
		if userID == ownerID {
			return Restrictions{}
		}
		return Restrictions{
			DisallowedServers: PrivateServers,
			AllowedTools:      ReadOnlyTools,
		}
	}
}
