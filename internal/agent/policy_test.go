package agent_test

import (
	"slices"
	"testing"

	"github.com/kvalv/kevinclaw/internal/agent"
)

func TestOwnerPolicy(t *testing.T) {
	policy := agent.NewOwnerPolicy("U_OWNER")

	t.Run("owner gets no restrictions", func(t *testing.T) {
		r := policy("U_OWNER", "C_ANY")
		if len(r.DisallowedServers) != 0 {
			t.Errorf("expected no blocked servers, got %v", r.DisallowedServers)
		}
		if r.AllowedTools != nil {
			t.Errorf("expected nil allowed tools, got %v", r.AllowedTools)
		}
	})

	t.Run("owner unrestricted in any channel", func(t *testing.T) {
		for _, ch := range []string{"C_PUBLIC", "C_PRIVATE", "D_DM", ""} {
			r := policy("U_OWNER", ch)
			if len(r.DisallowedServers) != 0 || r.AllowedTools != nil {
				t.Errorf("channel %q: expected no restrictions, got servers=%v tools=%v", ch, r.DisallowedServers, r.AllowedTools)
			}
		}
	})

	t.Run("non-owner gets restricted tools", func(t *testing.T) {
		r := policy("U_OTHER", "C_PUBLIC")
		if r.AllowedTools == nil {
			t.Fatal("expected allowed tools to be set")
		}
		for _, tool := range agent.ReadOnlyTools {
			if !slices.Contains(r.AllowedTools, tool) {
				t.Errorf("expected %q in allowed tools", tool)
			}
		}
	})

	t.Run("non-owner cannot access private servers", func(t *testing.T) {
		r := policy("U_OTHER", "C_PUBLIC")
		for _, server := range agent.PrivateServers {
			if !slices.Contains(r.DisallowedServers, server) {
				t.Errorf("expected %q to be blocked", server)
			}
		}
	})

	t.Run("non-owner cannot use Bash", func(t *testing.T) {
		r := policy("U_OTHER", "C_PUBLIC")
		if slices.Contains(r.AllowedTools, "Bash") {
			t.Error("Bash should not be allowed for non-owner")
		}
	})

	t.Run("non-owner cannot use Edit or Write", func(t *testing.T) {
		r := policy("U_OTHER", "C_PUBLIC")
		for _, tool := range []string{"Edit", "Write"} {
			if slices.Contains(r.AllowedTools, tool) {
				t.Errorf("%s should not be allowed for non-owner", tool)
			}
		}
	})

	t.Run("non-owner restricted in DM too", func(t *testing.T) {
		r := policy("U_OTHER", "D_DM_CHANNEL")
		if len(r.DisallowedServers) == 0 {
			t.Error("expected blocked servers in DM")
		}
		if r.AllowedTools == nil {
			t.Error("expected restricted tools in DM")
		}
	})
}
