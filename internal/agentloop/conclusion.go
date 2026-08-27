package agentloop

import "strings"

// DiagnosticConclusion is the conclusion an agent declares on a terminal turn
// typed TurnKindAnswer: the one cause it commits to, and the signals it
// considered and ruled out.
//
// It exists for the same reason TurnKind does, one level in. TurnKind made the
// SHAPE of a prose turn machine-readable because prose is not; this makes the
// turn's CONTENT machine-readable on the two axes an evaluation keys on. An
// evaluator that must decide whether an answer named a root cause, or dismissed
// a signal, has otherwise only the prose — and deciding it from prose means
// matching words, which measures vocabulary rather than reasoning. Two models
// answering the same question correctly in different words score differently,
// which is exactly the failure a benchmark comparing models cannot carry.
//
// The declaration is MODEL-AUTHORED, and that weakness is accepted rather than
// overlooked, on the same terms TurnKind accepted it: an agent whose declared
// RootCause its own prose contradicts defeats anything keyed on the
// declaration. What the declaration buys is that the agent's CLAIM becomes
// machine-readable. It is a signal, not a proof, and cross-checking a
// declaration against the text beside it is a consumer's concern, not joe's.
type DiagnosticConclusion struct {
	// RootCause is the one claim the agent commits to as the cause.
	//
	// The slot is for COMMITMENT, not for coverage: an enumeration of
	// possibilities is not a conclusion, and hedging belongs in the prose
	// beside it. An agent that will not commit leaves it empty, which is a
	// true report and a better one than crediting a hedge — a consumer reading
	// an empty RootCause reports the behaviour unassessable rather than
	// scoring it as a wrong diagnosis.
	RootCause string

	// Discarded holds the signals the agent considered and ruled out, in the
	// order it declared them. Empty is a real value: an agent that declared a
	// conclusion and discarded nothing has said something, and Declared is
	// what separates that from an agent that declared nothing at all.
	Discarded []DiscardedSignal
}

// DiscardedSignal is one signal the agent ruled out, with the rationale it gave
// for ruling it out.
//
// Both halves are required by the act being declared: the definition of
// discarding a signal is discarding it WITH stated rationale, so a signal named
// with no reason beside it is not the same act. A consumer therefore has the
// two facts separately rather than one string it would have to split again.
type DiscardedSignal struct {
	// Signal is the thing ruled out, in the agent's own words.
	Signal string
	// Rationale is why it was ruled out. Empty when the agent named a signal
	// and gave no reason — recorded as it stands rather than dropped, so a
	// consumer sees an incomplete declaration instead of no declaration.
	Rationale string
}

// Declared reports whether the agent emitted a diagnostic conclusion at all.
//
// It is the same distinction Session.TurnKindDeclared draws, and it is here for
// the same reason: an empty Discarded list is ambiguous between "discarded
// nothing" and "declared nothing", and no consumer can tell those apart from
// the list alone. The first is an answer — a FAIL for an assertion asking that
// a signal be discarded — and the second is an absence, which is unassessable.
// Scoring an absence as a wrong answer would make the resulting figure a
// measurement of contract adoption rather than of diagnostic accuracy.
func (c DiagnosticConclusion) Declared() bool {
	return c.RootCause != "" || len(c.Discarded) > 0
}

// The line prefixes the model emits to declare its conclusion, per the
// DIAGNOSTIC CONCLUSION clause in prompts.TaskSystem. They are matched the same
// way turnKindMarker is: case-insensitively, after stripping the emphasis
// characters a model tends to wrap a lone line in.
const (
	rootCauseMarker = "root-cause:"
	discardedMarker = "discarded:"
)

// discardedSeparator divides a DISCARDED line's signal from its rationale. A
// pipe rather than a dash or a colon because both of those occur inside the
// prose on either side of it — "user-service", "2 minutes ago: OOMKilled" —
// and a separator that appears in its own operands is not a separator.
const discardedSeparator = "|"

// SplitConclusion separates a declared diagnostic conclusion from the prose it
// was emitted alongside. It returns the conclusion and the content with every
// marker line removed.
//
// Marker lines are looked for on ANY line rather than only at the end, for the
// reason SplitTurnKind gives: models append trailing prose after a line they
// were told to put last often enough that anchoring to the final lines would
// drop the declaration and report a model that never declares.
//
// ROOT-CAUSE follows SplitTurnKind's LAST-one-wins rule — a model that restates
// its conclusion has refined it, and the refinement is the commitment. DISCARDED
// does not: it is a LIST, and every line is one entry, so they accumulate in
// declaration order. Those are different rules on purpose, and the difference is
// the difference between a field and a list.
//
// Every matched line is stripped whatever the turn's kind, because the markers
// are joe's plumbing and must not reach the operator, the history, or the intent
// probe under any kind. Whether a declaration MEANS anything on a turn that is
// not an answer is a consumer's question, and Declared plus the turn kind carry
// what it needs to decide.
func SplitConclusion(content string) (DiagnosticConclusion, string) {
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	var conclusion DiagnosticConclusion

	for _, line := range lines {
		trimmed := strings.Trim(strings.TrimSpace(line), "*_`# ")

		if rest, ok := cutMarker(trimmed, rootCauseMarker); ok {
			// An empty ROOT-CAUSE line is a marker with no commitment behind
			// it. The line is still stripped — it is plumbing either way — but
			// it does not overwrite a commitment made on an earlier line, and
			// it does not manufacture one.
			if rest != "" {
				conclusion.RootCause = rest
			}
			continue
		}

		if rest, ok := cutMarker(trimmed, discardedMarker); ok {
			if signal, rationale, present := parseDiscarded(rest); present {
				conclusion.Discarded = append(conclusion.Discarded, DiscardedSignal{
					Signal:    signal,
					Rationale: rationale,
				})
			}
			continue
		}

		kept = append(kept, line)
	}

	return conclusion, strings.TrimSpace(strings.Join(kept, "\n"))
}

// cutMarker reports whether a already-trimmed line opens with the given marker,
// returning what follows it with the quoting and emphasis a model wraps a value
// in removed.
func cutMarker(line, marker string) (string, bool) {
	if len(line) < len(marker) || !strings.EqualFold(line[:len(marker)], marker) {
		return "", false
	}
	return strings.Trim(strings.TrimSpace(line[len(marker):]), "*_`\" "), true
}

// parseDiscarded splits one DISCARDED line into its signal and rationale. The
// third return is false for a line carrying nothing at all, which is a marker
// with no entry behind it rather than an entry with no content.
//
// A line with no separator yields the whole of it as the signal and an EMPTY
// rationale, recorded rather than rejected. Dropping it would report an agent
// that named what it ruled out as having ruled out nothing, which is a worse
// misreading than an incomplete entry: a consumer that requires a rationale
// sees the entry and rejects it, and one that does not still learns the signal.
func parseDiscarded(rest string) (signal, rationale string, present bool) {
	if rest == "" {
		return "", "", false
	}
	before, after, found := strings.Cut(rest, discardedSeparator)
	if !found {
		return strings.TrimSpace(rest), "", true
	}
	signal = strings.TrimSpace(before)
	rationale = strings.TrimSpace(after)
	if signal == "" && rationale == "" {
		return "", "", false
	}
	return signal, rationale, true
}
