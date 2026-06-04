import { z } from 'zod';
import { apiClient } from './client';

// Wire schemas for the two Server-Sent Event payloads emitted by
// POST /api/v1/tasks/stream. The server emits exactly two event types —
// `step` (one per agentic-loop iteration) and `final` (terminal). There are
// no token-delta events and no `error` event: mid-stream failures arrive only
// inside `final.status` / `final.error`, and pre-stream validation failures
// arrive as a normal non-200 JSON body before any SSE byte.

const TokenUsageSchema = z.object({
  input_tokens: z.number(),
  output_tokens: z.number(),
});

const StepToolCallSchema = z.object({
  id: z.string(),
  name: z.string(),
  args: z.record(z.string(), z.unknown()).default({}),
});

const StepToolResultSchema = z.object({
  id: z.string(),
  name: z.string(),
  result: z.unknown().optional(),
  error: z.string().optional(),
  // Stable write-failure code for a denied tool call: 'zone_denial' or
  // 'incident_mode' (Item 8). Absent for a success or unclassified failure.
  error_code: z.string().optional(),
  duration_ms: z.number().optional(),
});

export const StepEventSchema = z.object({
  step_number: z.number(),
  llm_request: z.object({
    message_count: z.number(),
    tools_available: z.array(z.string()).default([]),
  }),
  llm_response: z.object({
    content: z.string().default(''),
    tool_calls: z.array(StepToolCallSchema).default([]),
    usage: TokenUsageSchema,
  }),
  tool_results: z.array(StepToolResultSchema).default([]),
});

// The seven terminal statuses the server can report on `final`. Only
// `completed` is success; the rest are failures surfaced to the user with
// `final.error`.
export const TaskStatusSchema = z.enum([
  'completed',
  'timeout',
  'max_iterations_reached',
  'runaway_terminated',
  'cost_limit_exceeded',
  'context_overflow',
  'error',
]);

export const FinalEventSchema = z.object({
  task_id: z.string(),
  session_id: z.string(),
  status: TaskStatusSchema,
  iterations: z.number(),
  steps: z.array(StepEventSchema).default([]),
  final_answer: z.string().default(''),
  tools_used: z.array(z.string()).default([]),
  total_tokens: TokenUsageSchema,
  duration_ms: z.number(),
  error: z.string().optional(),
  // Additive, optional history-pruning fields. The server omits them when
  // nothing was dropped (omitempty), so they default to false/0 here. When
  // history_trimmed is true, earlier messages fell out of the model's context
  // this turn and the UI surfaces an unobtrusive notice.
  history_trimmed: z.boolean().default(false),
  messages_dropped: z.number().default(0),
  // Per-message ingestion-truncation fields, also additive/optional. The
  // server omits them when nothing was truncated, so they default to 0/false.
  // tool_results_truncated needs no dedicated notice (the marker is visible
  // inside the rendered tool result); user_message_truncated drives an
  // unobtrusive notice that the message was shortened to fit the budget.
  tool_results_truncated: z.number().default(0),
  user_message_truncated: z.boolean().default(false),
  // Turn-level write-failure code (Item 8): the first per-tool denial code
  // seen this turn ('zone_denial' | 'incident_mode'). A denied write does NOT
  // terminate the loop, so this rides on an otherwise-completed turn and is
  // how the chat UI learns a write was refused and why. Absent when no write
  // was denied.
  error_code: z.string().optional(),
});

export type StepEvent = z.infer<typeof StepEventSchema>;
export type FinalEvent = z.infer<typeof FinalEventSchema>;
export type TaskStatus = z.infer<typeof TaskStatusSchema>;

// TaskStreamRequest is the body POSTed to /api/v1/tasks/stream. It mirrors the
// server's task request envelope minus the config block the UI does not set
// (and minus the deleted ClientTools field): a message plus an optional
// session id for multi-turn continuity.
export interface TaskStreamRequest {
  message: string;
  session_id?: string;
}

// RawFrame is one decoded SSE frame: an event name and its concatenated data.
export interface RawFrame {
  event: string;
  data: string;
}

// SSEFrameParser buffers stream chunks and yields complete SSE frames. A frame
// is terminated by a blank line (\n\n). Bytes that do not yet complete a frame
// are held in the buffer until their boundary arrives on a later chunk, so a
// frame split across two reads is reassembled correctly. Blank keep-alive
// lines and comment (`:`-prefixed) lines are ignored.
export class SSEFrameParser {
  private buffer = '';

  push(chunk: string): RawFrame[] {
    // Normalize CRLF so boundary detection is uniform regardless of the
    // server's line endings.
    this.buffer += chunk.replace(/\r\n/g, '\n');
    const frames: RawFrame[] = [];
    let boundary = this.buffer.indexOf('\n\n');
    while (boundary !== -1) {
      const raw = this.buffer.slice(0, boundary);
      this.buffer = this.buffer.slice(boundary + 2);
      const frame = parseFrame(raw);
      if (frame) frames.push(frame);
      boundary = this.buffer.indexOf('\n\n');
    }
    return frames;
  }
}

function parseFrame(raw: string): RawFrame | null {
  let event = '';
  const dataParts: string[] = [];
  for (const line of raw.split('\n')) {
    if (line === '' || line.startsWith(':')) {
      continue; // blank keep-alive line or comment
    }
    if (line.startsWith('event:')) {
      event = line.slice('event:'.length).trim();
    } else if (line.startsWith('data:')) {
      dataParts.push(line.slice('data:'.length).trim());
    }
  }
  if (event === '' && dataParts.length === 0) {
    return null;
  }
  return { event, data: dataParts.join('') };
}

export interface StreamHandlers {
  onStep: (step: StepEvent) => void;
  onFinal: (final: FinalEvent) => void;
  // onError carries `preStream: true` for a non-200 JSON error returned BEFORE
  // any SSE byte (the server's request-validation path — bad body, empty
  // message, LLM unavailable). `preStream: false` is a transport failure or an
  // unparseable/ malformed frame mid-stream. An in-stream agentic failure is
  // NOT an error here — it arrives as a `final` event with a non-completed
  // status and is delivered via onFinal. `code` is the server's typed error
  // code from a pre-stream JSON error body (e.g. zone_denial / incident_mode),
  // when present, so the caller can show a specific message.
  onError: (message: string, preStream: boolean, code?: string) => void;
}

// streamTask opens a streamed agentic turn against POST /api/v1/tasks/stream
// through the shared apiClient request path (so cookie + bearer auth both
// apply), then parses the SSE body incrementally, invoking the handlers as
// events arrive. It resolves when the stream ends or a terminal error is
// delivered; it never throws — all failure modes are routed through onError.
export async function streamTask(body: TaskStreamRequest, handlers: StreamHandlers): Promise<void> {
  let response: Response;
  try {
    response = await apiClient.requestRaw('/api/v1/tasks/stream', {
      method: 'POST',
      headers: { Accept: 'text/event-stream' },
      body: JSON.stringify(body),
    });
  } catch (err) {
    handlers.onError(err instanceof Error ? err.message : 'Could not reach the server', true);
    return;
  }

  // Pre-stream validation failure: a normal JSON error body, no SSE bytes.
  if (!response.ok) {
    let message = `Request failed (${response.status})`;
    let code: string | undefined;
    try {
      const errBody = (await response.json()) as { message?: string; error?: string };
      message = errBody.message ?? errBody.error ?? message;
      // `error` is the typed code (e.g. zone_denial / incident_mode); pass it
      // through so the caller can map it to a specific message.
      code = errBody.error;
    } catch {
      // Non-JSON error body — keep the status-based fallback.
    }
    handlers.onError(message, true, code);
    return;
  }

  if (!response.body) {
    handlers.onError('Streaming is not supported in this environment', false);
    return;
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  const parser = new SSEFrameParser();

  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      for (const frame of parser.push(decoder.decode(value, { stream: true }))) {
        dispatchFrame(frame, handlers);
      }
    }
    // Flush any bytes still buffered in the decoder / parser at stream end.
    for (const frame of parser.push(decoder.decode())) {
      dispatchFrame(frame, handlers);
    }
  } catch (err) {
    handlers.onError(err instanceof Error ? err.message : 'The stream was interrupted', false);
  }
}

function dispatchFrame(frame: RawFrame, handlers: StreamHandlers): void {
  if (!frame.data) return;

  let json: unknown;
  try {
    json = JSON.parse(frame.data);
  } catch {
    handlers.onError('Received an unparseable event from the server', false);
    return;
  }

  if (frame.event === 'step') {
    const parsed = StepEventSchema.safeParse(json);
    if (parsed.success) handlers.onStep(parsed.data);
    else handlers.onError('Received a malformed step event', false);
  } else if (frame.event === 'final') {
    const parsed = FinalEventSchema.safeParse(json);
    if (parsed.success) handlers.onFinal(parsed.data);
    else handlers.onError('Received a malformed final event', false);
  }
  // Unknown event names are ignored — forward compatibility with future events.
}
