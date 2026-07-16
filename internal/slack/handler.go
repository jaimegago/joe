package slack

import (
	"context"
	"log/slog"
	"strings"

	gslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// slackPoster is the subset of *gslack.Client used by Handler, allowing tests
// to inject a mock without a real Slack connection.
type slackPoster interface {
	PostMessage(channelID string, options ...gslack.MsgOption) (string, string, error)
}

// Handler dispatches Slack events to the appropriate command handler.
type Handler struct {
	api   slackPoster
	agent *Agent
	fmt   *Formatter
}

// NewHandler creates a Handler.
func NewHandler(api *gslack.Client, agent *Agent, fmt *Formatter) *Handler {
	return &Handler{api: api, agent: agent, fmt: fmt}
}

// HandleSlashCommand routes /joe slash commands.
func (h *Handler) HandleSlashCommand(ctx context.Context, cmd gslack.SlashCommand) {
	parts := strings.Fields(cmd.Text)
	var sub string
	if len(parts) > 0 {
		sub = strings.ToLower(parts[0])
	}

	switch sub {
	case "ask":
		query := strings.TrimSpace(strings.TrimPrefix(cmd.Text, parts[0]))
		if query == "" {
			h.postBlocks(cmd.ChannelID, h.fmt.ErrorBlock("Usage: `/joe ask <your question>`"))
			return
		}
		h.handleAsk(ctx, cmd.ChannelID, query)

	case "status":
		h.handleStatus(ctx, cmd.ChannelID)

	case "incidents":
		h.postBlocks(cmd.ChannelID, h.fmt.ErrorBlock(
			"Use `/joe ask 'show active incidents'` — or configure Alertmanager component IDs via `JOE_ALERTMANAGER_SOURCE`.",
		))

	case "help", "":
		h.postBlocks(cmd.ChannelID, h.fmt.HelpBlocks())

	default:
		// Treat the whole text as a query.
		h.handleAsk(ctx, cmd.ChannelID, cmd.Text)
	}
}

// HandleEventsAPI processes Slack Events API events (mentions, DMs).
func (h *Handler) HandleEventsAPI(ctx context.Context, sm *socketmode.Client, evt socketmode.Event) {
	sm.Ack(*evt.Request)

	eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		return
	}

	switch eventsAPIEvent.Type {
	case slackevents.CallbackEvent:
		h.handleCallbackEvent(ctx, eventsAPIEvent.InnerEvent)
	}
}

func (h *Handler) handleCallbackEvent(ctx context.Context, inner slackevents.EventsAPIInnerEvent) {
	switch ev := inner.Data.(type) {
	case *slackevents.AppMentionEvent:
		// Strip the bot mention prefix (@joe) from the text.
		query := strings.TrimSpace(stripMention(ev.Text))
		if query == "" {
			h.postBlocks(ev.Channel, h.fmt.HelpBlocks())
			return
		}
		h.handleAsk(ctx, ev.Channel, query)

	case *slackevents.MessageEvent:
		// Only handle DMs (channel_type == "im"), ignore bots.
		if ev.ChannelType != "im" || ev.BotID != "" || ev.SubType != "" {
			return
		}
		query := strings.TrimSpace(ev.Text)
		if query == "" {
			return
		}
		h.handleAsk(ctx, ev.Channel, query)
	}
}

// handleAsk runs the graph query and posts the result.
func (h *Handler) handleAsk(ctx context.Context, channelID, query string) {
	answer, err := h.agent.Ask(ctx, query)
	if err != nil {
		slog.Error("ask query failed", "error", err, "query", query)
		h.postBlocks(channelID, h.fmt.ErrorBlock("Query failed: "+err.Error()))
		return
	}
	h.postBlocks(channelID, h.fmt.AskBlocks(query, answer))
}

// handleStatus fetches and posts the graph summary.
func (h *Handler) handleStatus(ctx context.Context, channelID string) {
	summary, err := h.agent.Status(ctx)
	if err != nil {
		slog.Error("status query failed", "error", err)
		h.postBlocks(channelID, h.fmt.ErrorBlock("Could not retrieve status: "+err.Error()))
		return
	}
	h.postBlocks(channelID, h.fmt.StatusBlocks(summary))
}

// postBlocks sends a Block Kit message to the given channel.
func (h *Handler) postBlocks(channelID string, blocks []gslack.Block) {
	_, _, err := h.api.PostMessage(channelID, gslack.MsgOptionBlocks(blocks...))
	if err != nil {
		slog.Error("slack post message failed", "error", err, "channel", channelID)
	}
}

// stripMention removes the leading <@UXXXXXX> bot mention from text.
func stripMention(text string) string {
	if len(text) > 0 && text[0] == '<' {
		end := strings.IndexByte(text, '>')
		if end != -1 {
			return text[end+1:]
		}
	}
	return text
}
