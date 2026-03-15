package agent

import "fmt"

// Restrictions returned by a ToolPolicy.
type Restrictions struct {
	DisallowedServers []string // MCP server names to block
	AllowedTools      []string // if set, overrides default tool access
}

// ToolPolicy decides tool restrictions for a given message context.
type ToolPolicy func(userID, channel string) Restrictions

// PrivateServers is the list of MCP servers blocked for non-owners.
var PrivateServers = []string{"gcal", "linear", "cron", "debug", "homeassistant", "browser", "slack", "bugfix"}

// PolicyPaths defines directory access scopes.
type PolicyPaths struct {
	Write  []string // owner can edit
	Read   []string // owner can read
	Public []string // non-owner can read
}

// NewOwnerPolicy creates a policy where the owner gets full scoped access
// and everyone else is restricted to public paths with no private MCP servers.
func NewOwnerPolicy(ownerID string, paths PolicyPaths) ToolPolicy {
	ownerTools := buildOwnerTools(paths)
	publicTools := buildPublicTools(paths)

	return func(userID, channel string) Restrictions {
		if userID == ownerID {
			if len(ownerTools) > 0 {
				return Restrictions{AllowedTools: ownerTools}
			}
			return Restrictions{}
		}
		return Restrictions{
			DisallowedServers: PrivateServers,
			AllowedTools:      publicTools,
		}
	}
}

func buildOwnerTools(paths PolicyPaths) []string {
	if len(paths.Read) == 0 && len(paths.Write) == 0 {
		return nil // no restrictions
	}

	var tools []string
	for _, p := range paths.Write {
		tools = append(tools, fmt.Sprintf("Edit(%s/**)", p))
		tools = append(tools, fmt.Sprintf("Write(%s/**)", p))
	}
	for _, p := range paths.Read {
		tools = append(tools, fmt.Sprintf("Read(%s/**)", p))
		tools = append(tools, fmt.Sprintf("Glob(%s/**)", p))
		tools = append(tools, fmt.Sprintf("Grep(%s/**)", p))
	}
	// Unrestricted tools for owner
	tools = append(tools, "Bash", "WebSearch", "WebFetch", "Skill")
	return tools
}

func buildPublicTools(paths PolicyPaths) []string {
	if len(paths.Public) == 0 {
		return []string{"WebSearch"}
	}
	var tools []string
	for _, p := range paths.Public {
		tools = append(tools, fmt.Sprintf("Read(%s/**)", p))
		tools = append(tools, fmt.Sprintf("Glob(%s/**)", p))
		tools = append(tools, fmt.Sprintf("Grep(%s/**)", p))
	}
	tools = append(tools, "WebSearch")
	return tools
}
