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

// untitledSentinel is the reply the title prompt (prompts.ChatTitleSystem)
// instructs the model to return for an empty or meaningless opening message. It
// is intentionally identical to the UI's "New chat" placeholder, so it must NOT
// be persisted as a real title: doing so freezes the session at "New chat"
// (maybeAutoTitle only runs while the title is nil, so a later substantive
// message could never re-title it) and is indistinguishable from a genuinely
// untitled session. Treating it as "no title" leaves the row nil so the
// placeholder shows and the next turn gets another chance to title the session.
const untitledSentinel = "New chat"

// titleMaxTokens is the output-token budget for the title call. It is
// deliberately generous — far larger than a 3-6 word title needs — because
// reasoning models (e.g. Gemini 2.5) spend output budget on internal "thinking"
// before emitting any text. A tight cap (we shipped 32) was entirely consumed by
// thinking on Gemini, so the reply came back empty and the session stayed
// untitled. The reply is sanitized and length-capped regardless, so the extra
// headroom never yields a longer title.
const titleMaxTokens = 1024

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
			MaxTokens:    titleMaxTokens,
		})
		if err != nil || resp == nil {
			slog.Debug("auto-title: llm call failed", "session_id", sessionID, "error", err)
			return
		}
		title := sanitizeLLMTitle(resp.Content)
		if title == "" {
			slog.Debug("auto-title: llm returned an empty title", "session_id", sessionID)
			return
		}
		// The model emits the "New chat" sentinel for a meaningless opening
		// message. Persisting it would collide with the UI placeholder and
		// permanently freeze the session at "New chat" (the title would no longer
		// be nil, so no later turn could re-title it). Leave the session untitled
		// instead — the placeholder renders and the next turn retries.
		if strings.EqualFold(title, untitledSentinel) {
			slog.Debug("auto-title: llm returned the untitled sentinel; leaving untitled", "session_id", sessionID)
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
