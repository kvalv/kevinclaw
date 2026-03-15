// Smoke tests — verifies request/response marshaling against a fake server.
// Does not assert on request params or test error paths. The gcal package is
// a thin HTTP client; real coverage comes from integration tests against Google.
package gcal_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kvalv/kevinclaw/internal/gcal"
)

func TestGetEvents(t *testing.T) {
	ts := testServer()
	defer ts.Close()
	c := gcal.New("", "", "").WithBaseURL(ts.URL)

	events, err := c.GetEvents(t.Context(), "", "", "", "", 0)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Summary != "Standup" {
		t.Errorf("first event = %q, want Standup", events[0].Summary)
	}
}

func TestGetEvent(t *testing.T) {
	ts := testServer()
	defer ts.Close()
	c := gcal.New("", "", "").WithBaseURL(ts.URL)

	ev, err := c.GetEvent(t.Context(), "", "ev1")
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if ev.Summary != "Standup" {
		t.Errorf("summary = %q, want Standup", ev.Summary)
	}
	if ev.Description != "Daily standup" {
		t.Errorf("description = %q, want 'Daily standup'", ev.Description)
	}
}

func TestCreateEvent(t *testing.T) {
	ts := testServer()
	defer ts.Close()
	c := gcal.New("", "", "").WithBaseURL(ts.URL)

	created, err := c.CreateEvent(t.Context(), "", &gcal.Event{
		Summary: "New meeting",
		Start:   &gcal.EventTime{DateTime: "2026-03-15T14:00:00+01:00"},
		End:     &gcal.EventTime{DateTime: "2026-03-15T15:00:00+01:00"},
	})
	if err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if created.ID != "new1" {
		t.Errorf("id = %q, want new1", created.ID)
	}
}

func TestUpdateEvent(t *testing.T) {
	ts := testServer()
	defer ts.Close()
	c := gcal.New("", "", "").WithBaseURL(ts.URL)

	updated, err := c.UpdateEvent(t.Context(), "", "ev1", map[string]any{"summary": "Updated standup"})
	if err != nil {
		t.Fatalf("UpdateEvent: %v", err)
	}
	if updated.ID != "ev1" {
		t.Errorf("id = %q, want ev1", updated.ID)
	}
}

func TestDeleteEvent(t *testing.T) {
	ts := testServer()
	defer ts.Close()
	c := gcal.New("", "", "").WithBaseURL(ts.URL)

	err := c.DeleteEvent(t.Context(), "", "ev1")
	if err != nil {
		t.Fatalf("DeleteEvent: %v", err)
	}
}

func TestFreeBusy(t *testing.T) {
	ts := testServer()
	defer ts.Close()
	c := gcal.New("", "", "").WithBaseURL(ts.URL)

	raw, err := c.FreeBusy(t.Context(), "2026-03-15T00:00:00Z", "2026-03-16T00:00:00Z", nil)
	if err != nil {
		t.Fatalf("FreeBusy: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("empty response")
	}
}

// --- helpers ---

func testServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/events") && r.URL.Query().Get("singleEvents") != "":
			json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{"id": "ev1", "summary": "Standup", "start": map[string]string{"dateTime": "2026-03-15T09:00:00+01:00"}},
					{"id": "ev2", "summary": "Lunch", "start": map[string]string{"dateTime": "2026-03-15T12:00:00+01:00"}},
				},
			})
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/events/"):
			json.NewEncoder(w).Encode(map[string]any{
				"id": "ev1", "summary": "Standup", "description": "Daily standup",
				"start": map[string]string{"dateTime": "2026-03-15T09:00:00+01:00"},
				"end":   map[string]string{"dateTime": "2026-03-15T09:15:00+01:00"},
			})
		case r.Method == "POST" && strings.Contains(r.URL.Path, "/events"):
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			body["id"] = "new1"
			body["htmlLink"] = "https://calendar.google.com/event?eid=new1"
			json.NewEncoder(w).Encode(body)
		case r.Method == "PATCH":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			body["id"] = "ev1"
			json.NewEncoder(w).Encode(body)
		case r.Method == "DELETE":
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/freeBusy":
			json.NewEncoder(w).Encode(map[string]any{
				"calendars": map[string]any{
					"primary": map[string]any{
						"busy": []map[string]string{
							{"start": "2026-03-15T10:00:00Z", "end": "2026-03-15T11:00:00Z"},
						},
					},
				},
			})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
}
