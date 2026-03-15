package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/kvalv/kevinclaw/internal/config"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// categoryActions maps category to allowed actions.
var categoryActions = map[string][]string{
	"vacuum": {"start", "stop", "dock", "status"},
	"light":  {"turn_on", "turn_off", "status"},
	"sensor": {"status"},
	"cover":  {"open", "close", "stop", "status"},
}

// haClient is a thin Home Assistant REST API client.
type haClient struct {
	url   string
	token string
}

func (c *haClient) getState(ctx context.Context, entityID string) (json.RawMessage, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.url+"/api/states/"+entityID, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get state: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("get state %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (c *haClient) callService(ctx context.Context, domain, service, entityID string) error {
	data := fmt.Sprintf(`{"entity_id": %q}`, entityID)
	req, _ := http.NewRequestWithContext(ctx, "POST", c.url+"/api/services/"+domain+"/"+service, strings.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("call service: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("call service %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// serviceMap translates (category, action) to (domain, service).
func serviceMap(category, action string) (string, string, error) {
	switch category {
	case "vacuum":
		switch action {
		case "start":
			return "vacuum", "start", nil
		case "stop":
			return "vacuum", "stop", nil
		case "dock":
			return "vacuum", "return_to_base", nil
		}
	case "light":
		switch action {
		case "turn_on":
			return "light", "turn_on", nil
		case "turn_off":
			return "light", "turn_off", nil
		}
	case "cover":
		switch action {
		case "open":
			return "cover", "open_cover", nil
		case "close":
			return "cover", "close_cover", nil
		case "stop":
			return "cover", "stop_cover", nil
		}
	}
	return "", "", fmt.Errorf("unknown action %q for category %q", action, category)
}

// HomeAssistantServer creates an MCP server with Home Assistant tools based on config.
func HomeAssistantServer(cfgEntities []config.HAEntity, apiURL, apiToken string) *Server {
	s := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "kevinclaw-ha", Version: "v0.0.1"}, nil)
	client := &haClient{url: apiURL, token: apiToken}

	// Build entity lookup
	entities := make(map[string]config.HAEntity, len(cfgEntities))
	for _, e := range cfgEntities {
		entities[e.Name] = e
	}

	// Build descriptions for tool help
	var entityList []string
	for _, e := range cfgEntities {
		actions := categoryActions[e.Category]
		entityList = append(entityList, fmt.Sprintf("- %s (%s): %s [actions: %s]",
			e.Name, e.Category, e.Description, strings.Join(actions, ", ")))
	}
	entitiesHelp := strings.Join(entityList, "\n")

	s.AddTool(&sdkmcp.Tool{
		Name:        "ha_status",
		Description: "Get status of home devices.\n\nAvailable entities:\n" + entitiesHelp,
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {"type": "string", "description": "Entity name (omit for all)"}
			}
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			Name string `json:"name"`
		}
		json.Unmarshal(req.Params.Arguments, &args)

		if args.Name != "" {
			e, ok := entities[args.Name]
			if !ok {
				return errResult("unknown entity: %s", args.Name), nil
			}
			raw, err := client.getState(ctx, e.ID)
			if err != nil {
				return errResult("get state: %v", err), nil
			}
			var state struct {
				State      string         `json:"state"`
				Attributes map[string]any `json:"attributes"`
			}
			json.Unmarshal(raw, &state)
			return textResult("%s: %s %v", e.Name, state.State, state.Attributes), nil
		}

		// All entities
		var lines []string
		for _, e := range cfgEntities {
			raw, err := client.getState(ctx, e.ID)
			if err != nil {
				lines = append(lines, fmt.Sprintf("%s: error (%v)", e.Name, err))
				continue
			}
			var state struct {
				State string `json:"state"`
			}
			json.Unmarshal(raw, &state)
			lines = append(lines, fmt.Sprintf("%s: %s", e.Name, state.State))
		}
		return textResult("%s", strings.Join(lines, "\n")), nil
	})

	s.AddTool(&sdkmcp.Tool{
		Name:        "ha_action",
		Description: "Control a home device.\n\nAvailable entities:\n" + entitiesHelp,
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name":   {"type": "string", "description": "Entity name"},
				"action": {"type": "string", "description": "Action to perform"}
			},
			"required": ["name", "action"]
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			Name   string `json:"name"`
			Action string `json:"action"`
		}
		json.Unmarshal(req.Params.Arguments, &args)

		e, ok := entities[args.Name]
		if !ok {
			return errResult("unknown entity: %s", args.Name), nil
		}

		// Validate action
		allowed := categoryActions[e.Category]
		valid := false
		for _, a := range allowed {
			if a == args.Action {
				valid = true
				break
			}
		}
		if !valid {
			return errResult("action %q not allowed for %s (allowed: %s)", args.Action, e.Name, strings.Join(allowed, ", ")), nil
		}

		if args.Action == "status" {
			raw, err := client.getState(ctx, e.ID)
			if err != nil {
				return errResult("get state: %v", err), nil
			}
			var state struct {
				State      string         `json:"state"`
				Attributes map[string]any `json:"attributes"`
			}
			json.Unmarshal(raw, &state)
			return textResult("%s: %s", e.Name, state.State), nil
		}

		domain, service, err := serviceMap(e.Category, args.Action)
		if err != nil {
			return errResult("%v", err), nil
		}

		slog.Info("ha: calling service", "entity", e.Name, "domain", domain, "service", service)
		if err := client.callService(ctx, domain, service, e.ID); err != nil {
			return errResult("service call failed: %v", err), nil
		}
		return textResult("%s: %s done", e.Name, args.Action), nil
	})

	return s
}

// TryLoadHAConfig attempts to load ha.toml from the given path. Returns nil if not found.
