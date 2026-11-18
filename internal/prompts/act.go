package prompts

// The three parameters every tool that acts against a managed system carries
// beside its own (internal/tools/act.go). They are how the model states, with
// the call itself, why it is making it and what caused it; Joe records what it
// is given verbatim in the audit trail and writes nothing in the model's place
// (D-0170, D-0171).
//
// The wording is as short as it can be made and still be followed, and that is
// a constraint, not a style: these three descriptions are repeated on every
// managed-system tool definition, so each word here is paid for once per such
// tool on every request. Measured when they were written, on a JSON rendering
// of a task turn's tool schemas, a sentence-length wording added about 40 kB
// to the tool prefix and this one about 24 kB. Lengthen one only with that
// multiplication in mind.
const (
	// ActReasonParam describes the `reason` parameter.
	ActReasonParam = "Why you are making this call. Audited verbatim."

	// ActTriggerParam describes the `trigger` parameter. The values are the
	// audit trail's trigger vocabulary (audit.IsTriggerType); operator_request
	// is what an entry records when the parameter is omitted in a task turn.
	ActTriggerParam = "Cause, if not the operator's request: alert, policy or prior_finding."

	// ActTriggerRefParam describes the `trigger_ref` parameter.
	ActTriggerRefParam = "What the trigger names, e.g. alert/<name>."
)
