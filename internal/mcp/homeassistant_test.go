package mcp_test

import (
	"os"
	"testing"

	"github.com/kvalv/kevinclaw/internal/config"
	mcp "github.com/kvalv/kevinclaw/internal/mcp"
	"github.com/kvalv/kevinclaw/internal/testutil"
)

func TestHA_TurnOffLight(t *testing.T) {
	haURL := os.Getenv("HOMEASSISTANT_API_URL")
	if haURL == "" {
		haURL = "https://homeassistant.example.com" // dummy for replay
	}

	server := testutil.HTTPVCR(t, haURL)
	defer server.Close()

	entities := []config.HAEntity{
		{ID: "light.lys_over_spisebord", Name: "dining_table_light", Category: "light", Description: "Dining table light"},
	}

	token := os.Getenv("HOMEASSISTANT_API_TOKEN")
	if token == "" {
		token = "fake-token"
	}

	callTool, cleanup, err := mcp.TestClient(t.Context(), mcp.HomeAssistantServer(entities, server.URL, token))
	if err != nil {
		t.Fatalf("TestClient: %v", err)
	}
	defer cleanup()

	// Turn off
	result, err := callTool(t.Context(), "ha_action", map[string]any{
		"name":   "dining_table_light",
		"action": "turn_off",
	})
	if err != nil {
		t.Fatalf("ha_action: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool error: %v", result.Content)
	}
	t.Logf("result: %v", result.Content)

	// Get status
	result, err = callTool(t.Context(), "ha_status", map[string]any{
		"name": "dining_table_light",
	})
	if err != nil {
		t.Fatalf("ha_status: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool error: %v", result.Content)
	}
	t.Logf("status: %v", result.Content)
}
