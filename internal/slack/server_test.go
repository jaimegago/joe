package slack

import (
	"context"
	"testing"
	"time"

	gslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// newTestSocketModeClient creates a real *socketmode.Client with a buffered
// socketModeResponses channel so sm.Ack does not block or panic in tests.
func newTestSocketModeClient() *socketmode.Client {
	return socketmode.New(gslack.New("xoxb-fake-token"))
}

// TestNewHandler verifies the constructor returns a non-nil Handler.
func TestNewHandler(t *testing.T) {
	client := &mockJoeClient{}
	agent := NewAgent(client)
	// NewHandler accepts a *gslack.Client which we pass as nil — the constructor
	// merely stores the reference without dereferencing it.
	h := NewHandler(nil, agent, NewFormatter())
	if h == nil {
		t.Fatal("NewHandler returned nil")
	}
}

// TestNewServer verifies the constructor returns a non-nil Server.
func TestNewServer(t *testing.T) {
	agent := NewAgent(&mockJoeClient{})
	// nil *gslack.Client and *socketmode.Client are fine for construction.
	srv := NewServer((*gslack.Client)(nil), (*socketmode.Client)(nil), agent)
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
}

// TestDispatch_LoggingEvents verifies that logging-only event types (Connecting,
// Connected, Hello) don't panic and don't call sm.
func TestDispatch_LoggingEvents(t *testing.T) {
	poster := &mockSlackPoster{}
	handler := &Handler{api: poster, agent: NewAgent(&mockJoeClient{}), fmt: NewFormatter()}
	// sm is nil — these event types must not dereference sm.
	srv := &Server{sm: nil, handler: handler}

	loggingEvents := []socketmode.EventType{
		socketmode.EventTypeConnecting,
		socketmode.EventTypeConnected,
		socketmode.EventTypeHello,
	}

	for _, evtType := range loggingEvents {
		t.Run(string(evtType), func(t *testing.T) {
			// Must not panic.
			srv.dispatch(context.Background(), socketmode.Event{Type: evtType})
		})
	}
}

// TestDispatch_DefaultEvent verifies unhandled event types don't panic.
func TestDispatch_DefaultEvent(t *testing.T) {
	poster := &mockSlackPoster{}
	handler := &Handler{api: poster, agent: NewAgent(&mockJoeClient{}), fmt: NewFormatter()}
	srv := &Server{sm: nil, handler: handler}

	// A completely unknown event type — hits the default branch.
	srv.dispatch(context.Background(), socketmode.Event{Type: "unknown-event-type"})
}

// TestDispatch_SlashCommand_BadData verifies early return when the event
// data is not a SlashCommand (avoids calling sm.Ack).
func TestDispatch_SlashCommand_BadData(t *testing.T) {
	poster := &mockSlackPoster{}
	handler := &Handler{api: poster, agent: NewAgent(&mockJoeClient{}), fmt: NewFormatter()}
	srv := &Server{sm: nil, handler: handler}

	// Data is not a gslack.SlashCommand — handler returns before sm.Ack.
	srv.dispatch(context.Background(), socketmode.Event{
		Type: socketmode.EventTypeSlashCommand,
		Data: "not-a-slash-command",
	})
	// No panic = pass.
}

// TestProcessEvents_ClosedChannel verifies processEvents exits when the Events
// channel is closed (simulates socket disconnect).
func TestProcessEvents_ClosedChannel(t *testing.T) {
	poster := &mockSlackPoster{}
	handler := &Handler{api: poster, agent: NewAgent(&mockJoeClient{}), fmt: NewFormatter()}

	sm := &socketmode.Client{Events: make(chan socketmode.Event)}
	close(sm.Events)

	srv := &Server{sm: sm, handler: handler}
	// Closed channel returns immediately — no goroutine needed.
	srv.processEvents(context.Background())
}

// TestProcessEvents_DispatchesEvent verifies that an event delivered on the
// Events channel is passed to dispatch before the channel closes.
func TestProcessEvents_DispatchesEvent(t *testing.T) {
	poster := &mockSlackPoster{}
	handler := &Handler{api: poster, agent: NewAgent(&mockJoeClient{}), fmt: NewFormatter()}

	ch := make(chan socketmode.Event, 1)
	// A Connecting event just logs — no sm operations required.
	ch <- socketmode.Event{Type: socketmode.EventTypeConnecting}
	close(ch)

	sm := &socketmode.Client{Events: ch}
	srv := &Server{sm: sm, handler: handler}
	srv.processEvents(context.Background())
}

// TestProcessEvents_CtxCancel verifies processEvents exits when context is cancelled.
func TestProcessEvents_CtxCancel(t *testing.T) {
	poster := &mockSlackPoster{}
	handler := &Handler{api: poster, agent: NewAgent(&mockJoeClient{}), fmt: NewFormatter()}

	// Unbuffered channel — no events will be sent, so the ctx.Done() case fires.
	sm := &socketmode.Client{Events: make(chan socketmode.Event)}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	srv := &Server{sm: sm, handler: handler}
	srv.processEvents(ctx)
}

// TestDispatch_SlashCommand_ValidData covers the happy path of the SlashCommand case
// in dispatch — valid type assertion + sm.Ack + go HandleSlashCommand.
func TestDispatch_SlashCommand_ValidData(t *testing.T) {
	sm := newTestSocketModeClient()
	poster := &mockSlackPoster{posted: make(chan string, 1)}
	handler := &Handler{api: poster, agent: NewAgent(&mockJoeClient{}), fmt: NewFormatter()}
	srv := &Server{sm: sm, handler: handler}

	req := &socketmode.Request{EnvelopeID: "env-001"}
	cmd := gslack.SlashCommand{Text: "help", ChannelID: "C001"}
	evt := socketmode.Event{
		Type:    socketmode.EventTypeSlashCommand,
		Data:    cmd,
		Request: req,
	}
	srv.dispatch(context.Background(), evt)

	// Wait for the effect, not for the clock. dispatch spawns
	// `go s.handler.HandleSlashCommand`, and the poster call is the first
	// observable thing that goroutine does. The channel send also gives the
	// race detector a happens-before edge for the mock's fields, which a sleep
	// did not.
	select {
	case got := <-poster.posted:
		if got != "C001" {
			t.Errorf("PostMessage channel = %q, want C001", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("HandleSlashCommand did not reach the poster within 2s")
	}
}

// TestDispatch_EventsAPI covers the EventTypeEventsAPI case in dispatch.
func TestDispatch_EventsAPI(t *testing.T) {
	sm := newTestSocketModeClient()
	handler := &Handler{api: &mockSlackPoster{}, agent: NewAgent(&mockJoeClient{}), fmt: NewFormatter()}
	srv := &Server{sm: sm, handler: handler}

	req := &socketmode.Request{EnvelopeID: "env-002"}
	evt := socketmode.Event{
		Type:    socketmode.EventTypeEventsAPI,
		Data:    "not-an-events-api-event", // HandleEventsAPI's type assertion will fail → early return
		Request: req,
	}
	srv.dispatch(context.Background(), evt)

	// No wait. The Data here is deliberately not an EventsAPIEvent, so the
	// spawned HandleEventsAPI returns at its type assertion without touching
	// anything observable — there is nothing a wait could wait for. That early
	// return is covered synchronously by TestHandleEventsAPI_NotOK below, which
	// is where the assertion belongs. This test covers dispatch's routing only.
}

// TestHandleEventsAPI_NotOK covers the !ok early return when Data is not an EventsAPIEvent.
func TestHandleEventsAPI_NotOK(t *testing.T) {
	sm := newTestSocketModeClient()
	handler := &Handler{api: &mockSlackPoster{}, agent: NewAgent(&mockJoeClient{}), fmt: NewFormatter()}

	req := &socketmode.Request{EnvelopeID: "env-003"}
	evt := socketmode.Event{
		Data:    "not-an-events-api-event",
		Request: req,
	}
	handler.HandleEventsAPI(context.Background(), sm, evt)
}

// TestHandleEventsAPI_CallbackEvent covers the CallbackEvent switch arm in HandleEventsAPI.
func TestHandleEventsAPI_CallbackEvent(t *testing.T) {
	sm := newTestSocketModeClient()
	client := &mockJoeClient{}
	handler := &Handler{api: &mockSlackPoster{}, agent: NewAgent(client), fmt: NewFormatter()}

	req := &socketmode.Request{EnvelopeID: "env-004"}
	apiEvt := slackevents.EventsAPIEvent{
		Type: slackevents.CallbackEvent,
		InnerEvent: slackevents.EventsAPIInnerEvent{
			Data: &slackevents.AppMentionEvent{Text: "<@U1> hello", Channel: "CTEST"},
		},
	}
	evt := socketmode.Event{
		Data:    apiEvt,
		Request: req,
	}
	handler.HandleEventsAPI(context.Background(), sm, evt)
}
