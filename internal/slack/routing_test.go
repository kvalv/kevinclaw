package slack_test

import (
	"testing"

	"github.com/kvalv/kevinclaw/internal/slack"
)

func TestShouldProcess(t *testing.T) {
	active := []string{"C456"}

	tests := []struct {
		name string
		ev   slack.Event
		want bool
	}{
		{
			name: "channel, not tagged -> ignore",
			ev:   slack.Event{Channel: "C123", IsMention: false},
			want: false,
		},
		{
			name: "channel, tagged -> reply",
			ev:   slack.Event{Channel: "C123", IsMention: true},
			want: true,
		},
		{
			name: "DM -> reply",
			ev:   slack.Event{Channel: "D0AMF9GESNL", IsMention: false},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slack.ShouldHandle(tt.ev, active)
			if got != tt.want {
				t.Errorf("ShouldProcess() = %v, want %v", got, tt.want)
			}
		})
	}
}
