package slack

import (
	"context"
	"errors"
	"testing"

	gslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"

	"github.com/jaimegago/joe/internal/graph"
)

// mockSlackPoster captures PostMessage calls.
type mockSlackPoster struct {
	lastChannelID string
	callCount     int
	err           error

	// posted, when non-nil, receives each channel ID passed to PostMessage.
	// It exists so a test that dispatches into a goroutine can wait for the
	// call to actually happen rather than sleeping and hoping. Buffered by its
	// creator; the send is non-blocking so a test that stops reading cannot
	// wedge the handler.
	posted chan string
}

func (m *mockSlackPoster) PostMessage(channelID string, _ ...gslack.MsgOption) (string, string, error) {
	m.lastChannelID = channelID
	m.callCount++
	if m.posted != nil {
		select {
		case m.posted <- channelID:
		default:
		}
	}
	return "", "", m.err
}

// newTestHandler builds a Handler with a mock poster and mockJoeClient.
func newTestHandler(client *mockJoeClient) (*Handler, *mockSlackPoster) {
	poster := &mockSlackPoster{}
	agent := NewAgent(client)
	return &Handler{api: poster, agent: agent, fmt: NewFormatter()}, poster
}

func TestHandler_HandleSlashCommand_Ask(t *testing.T) {
	client := &mockJoeClient{
		nodes: []graph.Node{{ID: "payment-svc", Type: "deployment"}},
	}
	h, poster := newTestHandler(client)
	cmd := gslack.SlashCommand{Text: "ask show payment service", ChannelID: "C123"}
	h.HandleSlashCommand(context.Background(), cmd)
	if poster.callCount != 1 {
		t.Errorf("PostMessage called %d times, want 1", poster.callCount)
	}
	if poster.lastChannelID != "C123" {
		t.Errorf("channel = %q, want C123", poster.lastChannelID)
	}
}

func TestHandler_HandleSlashCommand_AskEmptyQuery(t *testing.T) {
	h, poster := newTestHandler(&mockJoeClient{})
	cmd := gslack.SlashCommand{Text: "ask", ChannelID: "C123"}
	h.HandleSlashCommand(context.Background(), cmd)
	if poster.callCount != 1 {
		t.Errorf("PostMessage called %d times, want 1 (error block)", poster.callCount)
	}
}

func TestHandler_HandleSlashCommand_Status(t *testing.T) {
	client := &mockJoeClient{
		summary: &graph.GraphSummary{NodeCount: 10, EdgeCount: 5},
	}
	h, poster := newTestHandler(client)
	cmd := gslack.SlashCommand{Text: "status", ChannelID: "C456"}
	h.HandleSlashCommand(context.Background(), cmd)
	if poster.callCount != 1 {
		t.Errorf("PostMessage called %d times, want 1", poster.callCount)
	}
}

func TestHandler_HandleSlashCommand_StatusError(t *testing.T) {
	client := &mockJoeClient{sumErr: errors.New("graph down")}
	h, poster := newTestHandler(client)
	cmd := gslack.SlashCommand{Text: "status", ChannelID: "C456"}
	h.HandleSlashCommand(context.Background(), cmd)
	if poster.callCount != 1 {
		t.Errorf("PostMessage called %d times, want 1 (error block)", poster.callCount)
	}
}

func TestHandler_HandleSlashCommand_Incidents(t *testing.T) {
	h, poster := newTestHandler(&mockJoeClient{})
	cmd := gslack.SlashCommand{Text: "incidents", ChannelID: "C789"}
	h.HandleSlashCommand(context.Background(), cmd)
	if poster.callCount != 1 {
		t.Errorf("PostMessage called %d times, want 1", poster.callCount)
	}
}

func TestHandler_HandleSlashCommand_Help(t *testing.T) {
	h, poster := newTestHandler(&mockJoeClient{})
	cmd := gslack.SlashCommand{Text: "help", ChannelID: "C789"}
	h.HandleSlashCommand(context.Background(), cmd)
	if poster.callCount != 1 {
		t.Errorf("PostMessage called %d times, want 1", poster.callCount)
	}
}

func TestHandler_HandleSlashCommand_EmptyText(t *testing.T) {
	h, poster := newTestHandler(&mockJoeClient{})
	cmd := gslack.SlashCommand{Text: "", ChannelID: "C789"}
	h.HandleSlashCommand(context.Background(), cmd)
	if poster.callCount != 1 {
		t.Errorf("PostMessage called %d times (help), want 1", poster.callCount)
	}
}

func TestHandler_HandleSlashCommand_UnknownTreatedAsAsk(t *testing.T) {
	client := &mockJoeClient{
		nodes: []graph.Node{{ID: "svc", Type: "service"}},
	}
	h, poster := newTestHandler(client)
	cmd := gslack.SlashCommand{Text: "show me all pods", ChannelID: "C999"}
	h.HandleSlashCommand(context.Background(), cmd)
	if poster.callCount != 1 {
		t.Errorf("PostMessage called %d times, want 1", poster.callCount)
	}
}

func TestHandler_HandleSlashCommand_AskError(t *testing.T) {
	client := &mockJoeClient{queryErr: errors.New("connection refused")}
	h, poster := newTestHandler(client)
	cmd := gslack.SlashCommand{Text: "ask find payment service", ChannelID: "C123"}
	h.HandleSlashCommand(context.Background(), cmd)
	if poster.callCount != 1 {
		t.Errorf("PostMessage called %d times, want 1 (error block)", poster.callCount)
	}
}

func TestHandler_PostBlocks(t *testing.T) {
	h, poster := newTestHandler(&mockJoeClient{})
	blocks := h.fmt.HelpBlocks()
	h.postBlocks("CCHAN", blocks)
	if poster.callCount != 1 {
		t.Errorf("PostMessage called %d times, want 1", poster.callCount)
	}
	if poster.lastChannelID != "CCHAN" {
		t.Errorf("channel = %q, want CCHAN", poster.lastChannelID)
	}
}

func TestHandler_PostBlocks_Error(t *testing.T) {
	poster := &mockSlackPoster{err: errors.New("slack API error")}
	h := &Handler{api: poster, agent: NewAgent(&mockJoeClient{}), fmt: NewFormatter()}
	// postBlocks logs the error but should not panic.
	h.postBlocks("CERR", h.fmt.HelpBlocks())
	if poster.callCount != 1 {
		t.Errorf("PostMessage called %d times, want 1", poster.callCount)
	}
}

func TestHandler_HandleCallbackEvent_AppMention(t *testing.T) {
	client := &mockJoeClient{
		nodes: []graph.Node{{ID: "svc", Type: "service"}},
	}
	h, poster := newTestHandler(client)

	inner := slackevents.EventsAPIInnerEvent{
		Data: &slackevents.AppMentionEvent{
			Text:    "<@U123> show payments",
			Channel: "CAPP",
		},
	}
	h.handleCallbackEvent(context.Background(), inner)
	if poster.callCount != 1 {
		t.Errorf("PostMessage called %d times, want 1", poster.callCount)
	}
}

func TestHandler_HandleCallbackEvent_AppMentionEmptyQuery(t *testing.T) {
	h, poster := newTestHandler(&mockJoeClient{})
	inner := slackevents.EventsAPIInnerEvent{
		Data: &slackevents.AppMentionEvent{
			Text:    "<@U123>",
			Channel: "CAPP",
		},
	}
	h.handleCallbackEvent(context.Background(), inner)
	// Should send help blocks.
	if poster.callCount != 1 {
		t.Errorf("PostMessage called %d times, want 1 (help)", poster.callCount)
	}
}

func TestHandler_HandleCallbackEvent_DirectMessage(t *testing.T) {
	client := &mockJoeClient{
		nodes: []graph.Node{{ID: "svc", Type: "service"}},
	}
	h, poster := newTestHandler(client)

	inner := slackevents.EventsAPIInnerEvent{
		Data: &slackevents.MessageEvent{
			Channel:     "CDM",
			ChannelType: "im",
			Text:        "show me the pods",
		},
	}
	h.handleCallbackEvent(context.Background(), inner)
	if poster.callCount != 1 {
		t.Errorf("PostMessage called %d times, want 1", poster.callCount)
	}
}

func TestHandler_HandleCallbackEvent_DirectMessageEmptyText(t *testing.T) {
	h, poster := newTestHandler(&mockJoeClient{})
	inner := slackevents.EventsAPIInnerEvent{
		Data: &slackevents.MessageEvent{
			Channel:     "CDM",
			ChannelType: "im",
			Text:        "",
		},
	}
	h.handleCallbackEvent(context.Background(), inner)
	// Empty text should be ignored, no PostMessage call.
	if poster.callCount != 0 {
		t.Errorf("PostMessage called %d times, want 0 for empty DM", poster.callCount)
	}
}

func TestHandler_HandleCallbackEvent_DirectMessageBot(t *testing.T) {
	h, poster := newTestHandler(&mockJoeClient{})
	inner := slackevents.EventsAPIInnerEvent{
		Data: &slackevents.MessageEvent{
			Channel:     "CDM",
			ChannelType: "im",
			BotID:       "B123",
			Text:        "I am a bot",
		},
	}
	h.handleCallbackEvent(context.Background(), inner)
	// Bot messages should be ignored.
	if poster.callCount != 0 {
		t.Errorf("PostMessage called %d times, want 0 for bot message", poster.callCount)
	}
}

func TestHandler_HandleCallbackEvent_NonIMMessage(t *testing.T) {
	h, poster := newTestHandler(&mockJoeClient{})
	inner := slackevents.EventsAPIInnerEvent{
		Data: &slackevents.MessageEvent{
			Channel:     "CCHAN",
			ChannelType: "channel", // not a DM
			Text:        "hello",
		},
	}
	h.handleCallbackEvent(context.Background(), inner)
	// Non-DM messages should be ignored.
	if poster.callCount != 0 {
		t.Errorf("PostMessage called %d times, want 0 for non-DM", poster.callCount)
	}
}

func TestHandler_HandleCallbackEvent_UnknownEventType(t *testing.T) {
	h, poster := newTestHandler(&mockJoeClient{})
	inner := slackevents.EventsAPIInnerEvent{
		// Data is nil / unknown type.
	}
	h.handleCallbackEvent(context.Background(), inner)
	if poster.callCount != 0 {
		t.Errorf("PostMessage called %d times, want 0 for unknown event", poster.callCount)
	}
}

func TestStripMention(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"<@U12345> show status", " show status"},
		{"<@UABC> ", " "},
		{"no mention here", "no mention here"},
		{"", ""},
		{"<@U123>", ""},
	}

	for _, tt := range tests {
		got := stripMention(tt.input)
		if got != tt.want {
			t.Errorf("stripMention(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildAskResponse_NoResults(t *testing.T) {
	got := buildAskResponse("unknown", nil)
	if !contains(got, "didn't find anything") {
		t.Errorf("expected no-results message, got: %q", got)
	}
}

func TestBuildAskResponse_NodesTruncatedAt5(t *testing.T) {
	nodes := make([]graph.Node, 8)
	for i := range nodes {
		nodes[i] = graph.Node{ID: "node", Type: "deployment"}
	}
	got := buildAskResponse("test", nodes)
	if !contains(got, "and 3 more") {
		t.Errorf("expected truncation note, got: %q", got)
	}
}

func TestBuildAskResponse_IncludesNodeID(t *testing.T) {
	nodes := []graph.Node{{ID: "payment-svc", Type: "deployment"}}
	got := buildAskResponse("payment", nodes)
	if !contains(got, "payment-svc") {
		t.Errorf("expected node ID in response, got: %q", got)
	}
}
