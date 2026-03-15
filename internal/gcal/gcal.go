package gcal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	calendarAPI = "https://www.googleapis.com/calendar/v3"
	tokenURL    = "https://oauth2.googleapis.com/token"
)

type Client struct {
	clientID     string
	clientSecret string
	refreshToken string
	baseURL      string

	mu           sync.Mutex
	accessToken  string
	tokenExpires time.Time
}

func New(clientID, clientSecret, refreshToken string) *Client {
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		refreshToken: refreshToken,
		baseURL:      calendarAPI,
	}
}

func (c *Client) WithBaseURL(url string) *Client {
	c.baseURL = url
	return c
}

func (c *Client) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Now().Before(c.tokenExpires.Add(-30*time.Second)) {
		return c.accessToken, nil
	}

	data := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"refresh_token": {c.refreshToken},
		"grant_type":    {"refresh_token"},
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", fmt.Errorf("decoding token: %w", err)
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("empty access token")
	}

	c.accessToken = tok.AccessToken
	c.tokenExpires = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	return c.accessToken, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var token string
	if c.baseURL == calendarAPI {
		var err error
		token, err = c.token(ctx)
		if err != nil {
			return nil, err
		}
	}

	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(b))
	}

	req, _ := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("api error %d: %s", resp.StatusCode, string(raw))
	}
	return raw, nil
}

// Event represents a Google Calendar event.
type Event struct {
	ID             string      `json:"id"`
	Summary        string      `json:"summary"`
	Description    string      `json:"description,omitempty"`
	Location       string      `json:"location,omitempty"`
	Status         string      `json:"status,omitempty"`
	Start          *EventTime  `json:"start,omitempty"`
	End            *EventTime  `json:"end,omitempty"`
	Attendees      []Attendee  `json:"attendees,omitempty"`
	Organizer      *Person     `json:"organizer,omitempty"`
	HangoutLink    string      `json:"hangoutLink,omitempty"`
	HTMLLink       string      `json:"htmlLink,omitempty"`
	ConferenceData *Conference `json:"conferenceData,omitempty"`
}

type EventTime struct {
	DateTime string `json:"dateTime,omitempty"`
	Date     string `json:"date,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

type Attendee struct {
	Email          string `json:"email"`
	ResponseStatus string `json:"responseStatus,omitempty"`
	DisplayName    string `json:"displayName,omitempty"`
}

type Person struct {
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

type Conference struct {
	EntryPoints []EntryPoint `json:"entryPoints,omitempty"`
}

type EntryPoint struct {
	EntryPointType string `json:"entryPointType"`
	URI            string `json:"uri"`
}

type EventList struct {
	Items []Event `json:"items"`
}

// GetEvents returns events from a calendar within a time range.
func (c *Client) GetEvents(ctx context.Context, calendarID, timeMin, timeMax, query string, maxResults int) ([]Event, error) {
	if calendarID == "" {
		calendarID = "primary"
	}
	if timeMin == "" {
		timeMin = time.Now().Format(time.RFC3339)
	}
	if timeMax == "" {
		timeMax = time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339)
	}
	if maxResults == 0 {
		maxResults = 25
	}

	params := url.Values{
		"timeMin":      {timeMin},
		"timeMax":      {timeMax},
		"maxResults":   {fmt.Sprintf("%d", maxResults)},
		"singleEvents": {"true"},
		"orderBy":      {"startTime"},
	}
	if query != "" {
		params.Set("q", query)
	}

	raw, err := c.do(ctx, "GET", "/calendars/"+url.PathEscape(calendarID)+"/events?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	var list EventList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("decoding events: %w", err)
	}
	return list.Items, nil
}

// GetEvent returns a single event by ID.
func (c *Client) GetEvent(ctx context.Context, calendarID, eventID string) (*Event, error) {
	if calendarID == "" {
		calendarID = "primary"
	}
	raw, err := c.do(ctx, "GET", "/calendars/"+url.PathEscape(calendarID)+"/events/"+url.PathEscape(eventID), nil)
	if err != nil {
		return nil, err
	}
	var ev Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, fmt.Errorf("decoding event: %w", err)
	}
	return &ev, nil
}

// CreateEvent creates a new event.
func (c *Client) CreateEvent(ctx context.Context, calendarID string, event *Event) (*Event, error) {
	if calendarID == "" {
		calendarID = "primary"
	}
	raw, err := c.do(ctx, "POST", "/calendars/"+url.PathEscape(calendarID)+"/events?sendUpdates=all", event)
	if err != nil {
		return nil, err
	}
	var created Event
	if err := json.Unmarshal(raw, &created); err != nil {
		return nil, fmt.Errorf("decoding created event: %w", err)
	}
	return &created, nil
}

// UpdateEvent updates an existing event. Only non-zero fields are sent.
func (c *Client) UpdateEvent(ctx context.Context, calendarID, eventID string, update map[string]any) (*Event, error) {
	if calendarID == "" {
		calendarID = "primary"
	}
	raw, err := c.do(ctx, "PATCH", "/calendars/"+url.PathEscape(calendarID)+"/events/"+url.PathEscape(eventID)+"?sendUpdates=all", update)
	if err != nil {
		return nil, err
	}
	var updated Event
	if err := json.Unmarshal(raw, &updated); err != nil {
		return nil, fmt.Errorf("decoding updated event: %w", err)
	}
	return &updated, nil
}

// DeleteEvent deletes an event.
func (c *Client) DeleteEvent(ctx context.Context, calendarID, eventID string) error {
	if calendarID == "" {
		calendarID = "primary"
	}
	_, err := c.do(ctx, "DELETE", "/calendars/"+url.PathEscape(calendarID)+"/events/"+url.PathEscape(eventID)+"?sendUpdates=all", nil)
	return err
}

// FreeBusy checks free/busy status for calendars.
func (c *Client) FreeBusy(ctx context.Context, timeMin, timeMax string, calendarIDs []string) (json.RawMessage, error) {
	if len(calendarIDs) == 0 {
		calendarIDs = []string{"primary"}
	}
	var items []map[string]string
	for _, id := range calendarIDs {
		items = append(items, map[string]string{"id": id})
	}
	body := map[string]any{
		"timeMin": timeMin,
		"timeMax": timeMax,
		"items":   items,
	}
	return c.do(ctx, "POST", "/freeBusy", body)
}
