package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	goslack "github.com/slack-go/slack"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// SlackServer creates an MCP server with Slack tools (send message, upload file, read channel).
func SlackServer(botToken string) *sdkmcp.Server {
	api := goslack.New(botToken)
	s := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "kevinclaw-slack", Version: "v0.0.1"}, nil)

	// User name cache for resolving IDs to display names
	userNames := make(map[string]string)
	resolveName := func(userID string) string {
		if name, ok := userNames[userID]; ok {
			return name
		}
		info, err := api.GetUserInfo(userID)
		if err != nil {
			userNames[userID] = userID
			return userID
		}
		name := info.Profile.DisplayName
		if name == "" {
			name = info.RealName
		}
		if name == "" {
			name = userID
		}
		userNames[userID] = name
		return name
	}

	s.AddTool(&sdkmcp.Tool{
		Name:        "slack_send_message",
		Description: "Send a message to a Slack channel or DM.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"channel":   {"type": "string", "description": "Channel ID or DM channel ID"},
				"text":      {"type": "string", "description": "Message text (Slack mrkdwn)"},
				"thread_ts": {"type": "string", "description": "Thread timestamp (optional, for replies)"}
			},
			"required": ["channel", "text"]
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			Channel  string `json:"channel"`
			Text     string `json:"text"`
			ThreadTS string `json:"thread_ts"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult("invalid arguments: %v", err), nil
		}

		opts := []goslack.MsgOption{goslack.MsgOptionText(args.Text, false)}
		if args.ThreadTS != "" {
			opts = append(opts, goslack.MsgOptionTS(args.ThreadTS))
		}

		_, ts, err := api.PostMessageContext(ctx, args.Channel, opts...)
		if err != nil {
			return errResult("send failed: %v", err), nil
		}
		return textResult("Message sent (ts=%s)", ts), nil
	})

	s.AddTool(&sdkmcp.Tool{
		Name:        "slack_upload_file",
		Description: "Upload a file to a Slack channel.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"file_path": {"type": "string", "description": "Absolute path to the file to upload"},
				"channel":   {"type": "string", "description": "Channel ID to upload to"},
				"message":   {"type": "string", "description": "Optional comment with the file"}
			},
			"required": ["file_path", "channel"]
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			FilePath string `json:"file_path"`
			Channel  string `json:"channel"`
			Message  string `json:"message"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult("invalid arguments: %v", err), nil
		}

		info, err := os.Stat(args.FilePath)
		if err != nil {
			return errResult("file not found: %v", err), nil
		}

		summary, err := api.UploadFileContext(ctx, goslack.UploadFileParameters{
			File:           args.FilePath,
			FileSize:       int(info.Size()),
			Filename:       info.Name(),
			Channel:        args.Channel,
			InitialComment: args.Message,
		})
		if err != nil {
			return errResult("upload failed: %v", err), nil
		}
		return textResult("File uploaded: %s", summary.ID), nil
	})

	s.AddTool(&sdkmcp.Tool{
		Name:        "slack_read_channel",
		Description: "Read recent messages from a Slack channel, including thread replies.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"channel": {"type": "string", "description": "Channel ID"},
				"limit":   {"type": "number", "description": "Max messages to fetch (default 20, max 100)"}
			},
			"required": ["channel"]
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			Channel string `json:"channel"`
			Limit   int    `json:"limit"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult("invalid arguments: %v", err), nil
		}
		if args.Limit <= 0 || args.Limit > 100 {
			args.Limit = 20
		}

		resp, err := api.GetConversationHistoryContext(ctx, &goslack.GetConversationHistoryParameters{
			ChannelID: args.Channel,
			Limit:     args.Limit,
		})
		if err != nil {
			return errResult("failed to read channel: %v", err), nil
		}

		var out strings.Builder
		// Messages come newest-first, reverse for chronological order
		for i := len(resp.Messages) - 1; i >= 0; i-- {
			msg := resp.Messages[i]
			fmt.Fprintf(&out, "[%s (%s) %s] %s\n", msg.User, resolveName(msg.User), msg.Timestamp, msg.Text)

			// Fetch thread replies if this message has a thread
			if msg.ThreadTimestamp != "" && msg.ThreadTimestamp == msg.Timestamp && msg.ReplyCount > 0 {
				replies, _, _, err := api.GetConversationRepliesContext(ctx, &goslack.GetConversationRepliesParameters{
					ChannelID: args.Channel,
					Timestamp: msg.ThreadTimestamp,
					Limit:     50,
				})
				if err == nil {
					for _, reply := range replies[1:] {
						fmt.Fprintf(&out, "  ↳ [%s (%s) %s] %s\n", reply.User, resolveName(reply.User), reply.Timestamp, reply.Text)
					}
				}
			}
		}

		return textResult("%s", out.String()), nil
	})

	return s
}
