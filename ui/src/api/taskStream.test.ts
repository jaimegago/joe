import { describe, it, expect, vi, beforeEach } from 'vitest';
import { SSEFrameParser, streamTask } from './taskStream';
import type { StepEvent, FinalEvent } from './taskStream';
import { apiClient } from './client';

// --- Fixtures -----------------------------------------------------------

function stepFrame(
  stepNumber: number,
  opts?: { input?: number; output?: number; tool?: string }
): string {
  const payload = {
    step_number: stepNumber,
    llm_request: { message_count: stepNumber, tools_available: ['k8s', 'metrics'] },
    llm_response: {
      content: `reasoning ${stepNumber}`,
      tool_calls: opts?.tool ? [{ id: `tc-${stepNumber}`, name: opts.tool, args: { q: 'x' } }] : [],
      usage: { input_tokens: opts?.input ?? 10, output_tokens: opts?.output ?? 5 },
    },
    tool_results: opts?.tool
      ? [{ id: `tc-${stepNumber}`, name: opts.tool, result: 'ok', duration_ms: 12 }]
      : [],
  };
  return `event: step\ndata: ${JSON.stringify(payload)}\n\n`;
}

function finalFrame(over?: Partial<FinalEvent>): string {
  const payload = {
    task_id: 't1',
    session_id: 's1',
    status: 'completed',
    iterations: 2,
    steps: [],
    final_answer: 'the answer',
    tools_used: ['k8s'],
    total_tokens: { input_tokens: 30, output_tokens: 15 },
    duration_ms: 100,
    ...over,
  };
  return `event: final\ndata: ${JSON.stringify(payload)}\n\n`;
}

// streamFromChunks builds a ReadableStream that emits the given string chunks
// as UTF-8, so streamTask exercises its real chunk-buffering read loop.
function streamFromChunks(chunks: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  let i = 0;
  return new ReadableStream<Uint8Array>({
    pull(controller) {
      if (i < chunks.length) {
        controller.enqueue(encoder.encode(chunks[i++]));
      } else {
        controller.close();
      }
    },
  });
}

// --- SSEFrameParser -----------------------------------------------------

describe('SSEFrameParser', () => {
  it('parses a well-formed step frame', () => {
    const parser = new SSEFrameParser();
    const frames = parser.push(stepFrame(1));
    expect(frames).toHaveLength(1);
    expect(frames[0].event).toBe('step');
    expect((JSON.parse(frames[0].data) as { step_number: number }).step_number).toBe(1);
  });

  it('parses a well-formed final frame', () => {
    const parser = new SSEFrameParser();
    const frames = parser.push(finalFrame());
    expect(frames).toHaveLength(1);
    expect(frames[0].event).toBe('final');
    expect((JSON.parse(frames[0].data) as { final_answer: string }).final_answer).toBe(
      'the answer'
    );
  });

  it('parses multiple frames delivered in one chunk', () => {
    const parser = new SSEFrameParser();
    const frames = parser.push(stepFrame(1) + stepFrame(2) + finalFrame());
    expect(frames.map((f) => f.event)).toEqual(['step', 'step', 'final']);
  });

  it('reassembles a frame split across two read chunks', () => {
    const parser = new SSEFrameParser();
    const whole = stepFrame(1);
    const splitAt = Math.floor(whole.length / 2);

    const first = parser.push(whole.slice(0, splitAt));
    expect(first).toHaveLength(0); // boundary not yet seen — buffered

    const second = parser.push(whole.slice(splitAt));
    expect(second).toHaveLength(1);
    expect((JSON.parse(second[0].data) as { step_number: number }).step_number).toBe(1);
  });

  it('ignores blank keep-alive lines and comments between frames', () => {
    const parser = new SSEFrameParser();
    // A lone comment+blank "keep-alive" frame, then a real step frame.
    const frames = parser.push(`: keep-alive\n\n` + stepFrame(7));
    expect(frames).toHaveLength(1);
    expect((JSON.parse(frames[0].data) as { step_number: number }).step_number).toBe(7);
  });
});

// --- streamTask ---------------------------------------------------------

describe('streamTask', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('dispatches step events then the final event over a chunked stream', async () => {
    vi.spyOn(apiClient, 'requestRaw').mockResolvedValue({
      ok: true,
      status: 200,
      body: streamFromChunks([stepFrame(1, { tool: 'k8s' }), stepFrame(2), finalFrame()]),
    } as unknown as Response);

    const steps: StepEvent[] = [];
    let final: FinalEvent | null = null;
    const onError = vi.fn();

    await streamTask(
      { session_id: 's1', message: 'hi' },
      { onStep: (s) => steps.push(s), onFinal: (f) => (final = f), onError }
    );

    expect(steps.map((s) => s.step_number)).toEqual([1, 2]);
    expect(final).not.toBeNull();
    expect(final!.final_answer).toBe('the answer');
    expect(onError).not.toHaveBeenCalled();
  });

  it('reassembles a step frame split across read chunks', async () => {
    const whole = stepFrame(1);
    const cut = Math.floor(whole.length / 2);
    vi.spyOn(apiClient, 'requestRaw').mockResolvedValue({
      ok: true,
      status: 200,
      body: streamFromChunks([whole.slice(0, cut), whole.slice(cut), finalFrame()]),
    } as unknown as Response);

    const steps: StepEvent[] = [];
    await streamTask(
      { message: 'hi' },
      { onStep: (s) => steps.push(s), onFinal: vi.fn(), onError: vi.fn() }
    );
    expect(steps).toHaveLength(1);
    expect(steps[0].step_number).toBe(1);
  });

  it('surfaces a pre-stream non-200 JSON error and reads no stream', async () => {
    vi.spyOn(apiClient, 'requestRaw').mockResolvedValue({
      ok: false,
      status: 400,
      json: () => Promise.resolve({ error: 'invalid_request', message: 'message is required' }),
    } as unknown as Response);

    const onStep = vi.fn();
    const onFinal = vi.fn();
    const onError = vi.fn();

    await streamTask({ message: '' }, { onStep, onFinal, onError });

    // The typed code from the error body (`error`) rides along as the third arg.
    expect(onError).toHaveBeenCalledWith('message is required', true, 'invalid_request');
    expect(onStep).not.toHaveBeenCalled();
    expect(onFinal).not.toHaveBeenCalled();
  });

  it('reports a transport failure as a non-pre-stream error', async () => {
    vi.spyOn(apiClient, 'requestRaw').mockRejectedValue(new Error('network down'));

    const onError = vi.fn();
    await streamTask({ message: 'hi' }, { onStep: vi.fn(), onFinal: vi.fn(), onError });

    expect(onError).toHaveBeenCalledWith('network down', true);
  });

  it('delivers an in-stream final with status=error via onFinal, not onError', async () => {
    vi.spyOn(apiClient, 'requestRaw').mockResolvedValue({
      ok: true,
      status: 200,
      body: streamFromChunks([finalFrame({ status: 'error', error: 'boom', final_answer: '' })]),
    } as unknown as Response);

    let final: FinalEvent | null = null;
    const onError = vi.fn();
    await streamTask({ message: 'hi' }, { onStep: vi.fn(), onFinal: (f) => (final = f), onError });

    expect(onError).not.toHaveBeenCalled();
    expect(final!.status).toBe('error');
    expect(final!.error).toBe('boom');
  });
});
