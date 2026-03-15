package mcp

import (
	"context"
	"encoding/json"

	"github.com/kvalv/kevinclaw/internal/gcal"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// GCalServer creates an MCP server with Google Calendar tools.
func GCalServer(client *gcal.Client) *Server {
	s := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "kevinclaw-gcal", Version: "v0.0.1"}, nil)

	s.AddTool(&sdkmcp.Tool{
		Name:        "gcal_get_events",
		Description: "Get upcoming events from a calendar.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"calendar_id": {"type": "string", "description": "Calendar ID (default: primary)"},
				"time_min":    {"type": "string", "description": "Start time ISO 8601 with timezone. Defaults to now."},
				"time_max":    {"type": "string", "description": "End time ISO 8601. Defaults to 7 days from now."},
				"max_results": {"type": "number", "description": "Max events to return (default: 25)"},
				"query":       {"type": "string", "description": "Free-text search filter"}
			}
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			CalendarID string `json:"calendar_id"`
			TimeMin    string `json:"time_min"`
			TimeMax    string `json:"time_max"`
			MaxResults int    `json:"max_results"`
			Query      string `json:"query"`
		}
		json.Unmarshal(req.Params.Arguments, &args)

		events, err := client.GetEvents(ctx, args.CalendarID, args.TimeMin, args.TimeMax, args.Query, args.MaxResults)
		if err != nil {
			return errResult("get events: %v", err), nil
		}
		out, _ := json.Marshal(events)
		return textResult("%s", string(out)), nil
	})

	s.AddTool(&sdkmcp.Tool{
		Name:        "gcal_get_event",
		Description: "Get full details of a specific event.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"calendar_id": {"type": "string", "description": "Calendar ID (default: primary)"},
				"event_id":    {"type": "string", "description": "The event ID"}
			},
			"required": ["event_id"]
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			CalendarID string `json:"calendar_id"`
			EventID    string `json:"event_id"`
		}
		json.Unmarshal(req.Params.Arguments, &args)

		event, err := client.GetEvent(ctx, args.CalendarID, args.EventID)
		if err != nil {
			return errResult("get event: %v", err), nil
		}
		out, _ := json.Marshal(event)
		return textResult("%s", string(out)), nil
	})

	s.AddTool(&sdkmcp.Tool{
		Name:        "gcal_create_event",
		Description: "Create a new calendar event.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"calendar_id": {"type": "string", "description": "Calendar ID (default: primary)"},
				"summary":     {"type": "string", "description": "Event title"},
				"start":       {"type": "string", "description": "Start time ISO 8601 with timezone, or date for all-day (e.g. 2026-03-15)"},
				"end":         {"type": "string", "description": "End time, same format as start"},
				"description": {"type": "string", "description": "Event description"},
				"location":    {"type": "string", "description": "Event location"},
				"attendees":   {"type": "array", "items": {"type": "string"}, "description": "Email addresses of attendees"}
			},
			"required": ["summary", "start", "end"]
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			CalendarID  string   `json:"calendar_id"`
			Summary     string   `json:"summary"`
			Start       string   `json:"start"`
			End         string   `json:"end"`
			Description string   `json:"description"`
			Location    string   `json:"location"`
			Attendees   []string `json:"attendees"`
		}
		json.Unmarshal(req.Params.Arguments, &args)

		event := &gcal.Event{
			Summary:     args.Summary,
			Description: args.Description,
			Location:    args.Location,
			Start:       parseEventTime(args.Start),
			End:         parseEventTime(args.End),
		}
		for _, email := range args.Attendees {
			event.Attendees = append(event.Attendees, gcal.Attendee{Email: email})
		}

		created, err := client.CreateEvent(ctx, args.CalendarID, event)
		if err != nil {
			return errResult("create event: %v", err), nil
		}
		out, _ := json.Marshal(created)
		return textResult("%s", string(out)), nil
	})

	s.AddTool(&sdkmcp.Tool{
		Name:        "gcal_update_event",
		Description: "Update an existing calendar event.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"calendar_id": {"type": "string", "description": "Calendar ID (default: primary)"},
				"event_id":    {"type": "string", "description": "Event ID to update"},
				"summary":     {"type": "string", "description": "New title"},
				"start":       {"type": "string", "description": "New start time ISO 8601"},
				"end":         {"type": "string", "description": "New end time ISO 8601"},
				"description": {"type": "string", "description": "New description"},
				"location":    {"type": "string", "description": "New location"}
			},
			"required": ["event_id"]
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			CalendarID  string `json:"calendar_id"`
			EventID     string `json:"event_id"`
			Summary     string `json:"summary"`
			Start       string `json:"start"`
			End         string `json:"end"`
			Description string `json:"description"`
			Location    string `json:"location"`
		}
		json.Unmarshal(req.Params.Arguments, &args)

		update := make(map[string]any)
		if args.Summary != "" {
			update["summary"] = args.Summary
		}
		if args.Description != "" {
			update["description"] = args.Description
		}
		if args.Location != "" {
			update["location"] = args.Location
		}
		if args.Start != "" {
			update["start"] = parseEventTime(args.Start)
		}
		if args.End != "" {
			update["end"] = parseEventTime(args.End)
		}

		updated, err := client.UpdateEvent(ctx, args.CalendarID, args.EventID, update)
		if err != nil {
			return errResult("update event: %v", err), nil
		}
		out, _ := json.Marshal(updated)
		return textResult("%s", string(out)), nil
	})

	s.AddTool(&sdkmcp.Tool{
		Name:        "gcal_delete_event",
		Description: "Delete a calendar event.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"calendar_id": {"type": "string", "description": "Calendar ID (default: primary)"},
				"event_id":    {"type": "string", "description": "Event ID to delete"}
			},
			"required": ["event_id"]
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			CalendarID string `json:"calendar_id"`
			EventID    string `json:"event_id"`
		}
		json.Unmarshal(req.Params.Arguments, &args)

		if err := client.DeleteEvent(ctx, args.CalendarID, args.EventID); err != nil {
			return errResult("delete event: %v", err), nil
		}
		return textResult("Event deleted."), nil
	})

	s.AddTool(&sdkmcp.Tool{
		Name:        "gcal_freebusy",
		Description: "Check free/busy status across calendars.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"time_min":     {"type": "string", "description": "Start time ISO 8601"},
				"time_max":     {"type": "string", "description": "End time ISO 8601"},
				"calendar_ids": {"type": "array", "items": {"type": "string"}, "description": "Calendar IDs to check (default: primary)"}
			},
			"required": ["time_min", "time_max"]
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			TimeMin     string   `json:"time_min"`
			TimeMax     string   `json:"time_max"`
			CalendarIDs []string `json:"calendar_ids"`
		}
		json.Unmarshal(req.Params.Arguments, &args)

		raw, err := client.FreeBusy(ctx, args.TimeMin, args.TimeMax, args.CalendarIDs)
		if err != nil {
			return errResult("freebusy: %v", err), nil
		}
		return textResult("%s", string(raw)), nil
	})

	return s
}

// parseEventTime converts a time string to an EventTime.
// If it looks like a date (len 10), uses Date field; otherwise DateTime.
func parseEventTime(s string) *gcal.EventTime {
	if len(s) == 10 {
		return &gcal.EventTime{Date: s}
	}
	return &gcal.EventTime{DateTime: s}
}
