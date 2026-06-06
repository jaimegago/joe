package api

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/prompts"
)

// maxTitleLen caps a generated title so a verbose model reply cannot produce an
// unbounded sidebar/header label.
const maxTitleLen = 60

// maybeAutoTitle gives a freshly-started session its first title. It runs on the
// turn that persists the opening message: if the session still has no title, it
// kicks off an async LLM call to generate one (claude.ai-style — the session
// shows a "New chat" placeholder in the UI until the title lands, with no
// raw-first-words intermediate state). A session that already has a title — a
// later turn, or a user-set name — is left untouched, so this never clobbers a
// manual rename.
func (h *taskHandler) maybeAutoTitle(ctx context.Context, sessionID, firstUserMsg string) {
	if h.server.services.SessionModel == nil {
		return
	}
	sess, err := h.server.services.SessionModel.GetSession(ctx, sessionID)
	if err != nil || sess == nil || sess.Title != nil {
		return
	}
	h.generateTitleAsync(ctx, sessionID, firstUserMsg)
}

// generateTitleAsync produces the session's title from its opening message via a
// small LLM call, in the background. It detaches from the request's cancellation
// (the HTTP turn returns immediately) while preserving the principal/session
// context values, and bounds itself with its own timeout. The write is
// conditional: it only sets the title if the session is still untitled — a user
// who renamed in the meantime wins, and a second turn never overwrites. The UI
// polls the session after the first turn and swaps the placeholder for this
// title once it lands. Best-effort throughout; any failure leaves the session
// untitled (the placeholder persists), so a degraded/unconfigured LLM never
// blocks the turn.
func (h *taskHandler) generateTitleAsync(ctx context.Context, sessionID, firstUserMsg string) {
	if h.server.services.LLM == nil {
		return
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Warn("auto-title: generation panicked", "session_id", sessionID, "panic", r)
			}
		}()

		tctx, cancel := context.WithTimeout(bg, 30*time.Second)
		defer cancel()

		resp, err := h.server.services.LLM.Chat(tctx, llm.ChatRequest{
			SystemPrompt: prompts.ChatTitleSystem,
			Messages:     []llm.Message{{Role: "user", Content: firstUserMsg}},
			MaxTokens:    32,
		})
		if err != nil || resp == nil {
			return
		}
		title := sanitizeLLMTitle(resp.Content)
		if title == "" {
			return
		}

		// Only set the title if the session is still untitled — respect a manual
		// rename that landed while the LLM call was in flight, and stay idempotent
		// across concurrent first turns.
		cur, err := h.server.services.SessionModel.GetSession(tctx, sessionID)
		if err != nil || cur == nil || cur.Title != nil {
			return
		}
		if err := h.server.services.SessionModel.UpdateSessionTitle(tctx, sessionID, title); err != nil {
			slog.Debug("auto-title: write failed", "session_id", sessionID, "error", err)
		}
	}()
}

// sanitizeLLMTitle cleans a model's raw title reply into a single-line label:
// it keeps the first non-empty line, strips wrapping quotes and trailing
// punctuation, and caps the length. Returns "" for an empty or sentinel reply.
func sanitizeLLMTitle(raw string) string {
	title := strings.TrimSpace(raw)
	if title == "" {
		return ""
	}
	// Some models prepend a line or wrap the title in quotes — keep the first
	// non-empty line and unwrap.
	for line := range strings.SplitSeq(title, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			title = s
			break
		}
	}
	title = strings.Trim(title, `"'`)
	title = strings.TrimRight(title, ".!?")
	title = strings.TrimSpace(title)
	if len(title) > maxTitleLen {
		title = strings.TrimSpace(title[:maxTitleLen])
		if i := strings.LastIndex(title, " "); i > 0 {
			title = title[:i]
		}
	}
	return title
}
