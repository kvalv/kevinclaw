package agent_test

import (
	"slices"
	"testing"

	"github.com/kvalv/kevinclaw/internal/agent"
)

func TestOwnerPolicy(t *testing.T) {
	policy := agent.NewOwnerPolicy("U_OWNER", agent.PolicyPaths{
		Read:   []string{"~/src/main/a", "~/scripts"},
		Write:  []string{"~/src/main/a"},
		Public: []string{"~/src/main/a/docs"},
	})

	t.Run("owner gets scoped access", func(t *testing.T) {
		r := policy("U_OWNER", "C_ANY")
		if len(r.DisallowedServers) != 0 {
			t.Errorf("expected no blocked servers, got %v", r.DisallowedServers)
		}
		if r.AllowedTools == nil {
			t.Fatal("expected scoped tools for owner")
		}
		// Should have Edit/Write for write paths
		if !slices.ContainsFunc(r.AllowedTools, func(s string) bool { return s == "Edit(~/src/main/a/**)" }) {
			t.Errorf("expected Edit for write path, got %v", r.AllowedTools)
		}
		// Should have Read for read paths
		if !slices.ContainsFunc(r.AllowedTools, func(s string) bool { return s == "Read(~/scripts/**)" }) {
			t.Errorf("expected Read for read path, got %v", r.AllowedTools)
		}
		// Should have Bash, Skill
		if !slices.Contains(r.AllowedTools, "Bash") {
			t.Error("expected Bash for owner")
		}
		if !slices.Contains(r.AllowedTools, "Skill") {
			t.Error("expected Skill for owner")
		}
	})

	t.Run("non-owner gets public path access only", func(t *testing.T) {
		r := policy("U_OTHER", "C_PUBLIC")
		if r.AllowedTools == nil {
			t.Fatal("expected allowed tools to be set")
		}
		// Should have scoped Read for public paths
		if !slices.ContainsFunc(r.AllowedTools, func(s string) bool { return s == "Read(~/src/main/a/docs/**)" }) {
			t.Errorf("expected scoped Read for public path, got %v", r.AllowedTools)
		}
		// Should have WebSearch
		if !slices.Contains(r.AllowedTools, "WebSearch") {
			t.Error("expected WebSearch")
		}
		// Should NOT have unscoped Read
		if slices.Contains(r.AllowedTools, "Read") {
			t.Error("should not have unscoped Read")
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
