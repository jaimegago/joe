package api

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/prompts"
)

// maxHeuristicTitleLen caps the immediate first-words title so a long opening
// message cannot produce an unbounded sidebar label. The async LLM upgrade
// usually replaces it with something tighter.
const maxHeuristicTitleLen = 60

// heuristicTitle distils a chat's first user message into an immediate title:
// collapse whitespace, take the first six words, and cap the length. This is
// the synchronous "title immediately" half of DESIGN-CHAT-SESSIONS.md §11
// Phase 2 (the LLM upgrade is the async half). Returns "" when the message has
// no usable words, in which case the caller leaves the session untitled rather
// than writing a blank label.
func heuristicTitle(msg string) string {
	fields := strings.Fields(msg)
	if len(fields) == 0 {
		return ""
	}
	if len(fields) > 6 {
		fields = fields[:6]
	}
	title := strings.Join(fields, " ")
	if len(title) > maxHeuristicTitleLen {
		// Trim to the cap on a rune boundary, then drop any partial trailing
		// word so the title never ends mid-word.
		title = strings.TrimSpace(title[:maxHeuristicTitleLen])
		if i := strings.LastIndex(title, " "); i > 0 {
			title = title[:i]
		}
	}
	return title
}

// maybeAutoTitle gives a freshly-started session its first title. It runs on the
// turn that persists the opening message: if the session still has no title, it
// writes the first-words heuristic synchronously (so the browse list and
// dashboard label it right away) and then kicks off an async LLM upgrade. A
// session that already has a title — a later turn, or a user-set name — is left
// untouched, so this never clobbers a manual rename.
func (h *taskHandler) maybeAutoTitle(ctx context.Context, sessionID, firstUserMsg string) {
	if h.server.services.SessionModel == nil {
		return
	}
	sess, err := h.server.services.SessionModel.GetSession(ctx, sessionID)
	if err != nil || sess == nil || sess.Title != nil {
		return
	}

	heuristic := heuristicTitle(firstUserMsg)
	if heuristic == "" {
		return
	}
	if err := h.server.services.SessionModel.UpdateSessionTitle(ctx, sessionID, heuristic); err != nil {
		slog.Warn("auto-title: heuristic write failed", "session_id", sessionID, "error", err)
		return
	}

	h.upgradeTitleAsync(ctx, sessionID, firstUserMsg, heuristic)
}

// upgradeTitleAsync replaces the heuristic title with an LLM-generated one in
// the background. It detaches from the request's cancellation (the HTTP turn
// returns immediately) while preserving the principal/session context values,
// and bounds itself with its own timeout. The upgrade is conditional: it only
// overwrites the title if it is still the heuristic we set — a user who renamed
// in the meantime wins. Best-effort throughout; any failure leaves the
// heuristic in place.
func (h *taskHandler) upgradeTitleAsync(ctx context.Context, sessionID, firstUserMsg, heuristic string) {
	if h.server.services.LLM == nil {
		return
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Warn("auto-title: upgrade panicked", "session_id", sessionID, "panic", r)
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
		if title == "" || title == heuristic {
			return
		}

		// Only upgrade if the title is still the heuristic — respect a manual
		// rename that landed while the LLM call was in flight.
		cur, err := h.server.services.SessionModel.GetSession(tctx, sessionID)
		if err != nil || cur == nil || cur.Title == nil || *cur.Title != heuristic {
			return
		}
		if err := h.server.services.SessionModel.UpdateSessionTitle(tctx, sessionID, title); err != nil {
			slog.Debug("auto-title: upgrade write failed", "session_id", sessionID, "error", err)
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
	if len(title) > maxHeuristicTitleLen {
		title = strings.TrimSpace(title[:maxHeuristicTitleLen])
		if i := strings.LastIndex(title, " "); i > 0 {
			title = title[:i]
		}
	}
	return title
}
