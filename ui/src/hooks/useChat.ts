import { useState, useCallback, useMemo } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { sendMessage, fetchMessages, createSession } from '@/api/chat';
import type { ChatMessage } from '@/api/types';

export function useChat(initialSessionId?: string) {
  const qc = useQueryClient();
  const [sessionId, setSessionId] = useState<string | null>(initialSessionId ?? null);
  const [isSending, setIsSending] = useState(false);
  const [sendError, setSendError] = useState<string | null>(null);
  // Optimistic messages shown immediately while the LLM processes
  const [pending, setPending] = useState<ChatMessage[]>([]);

  const messagesQ = useQuery({
    queryKey: ['messages', sessionId],
    queryFn: () => fetchMessages(sessionId!),
    enabled: sessionId != null,
  });

  const send = useCallback(
    async (content: string) => {
      setIsSending(true);
      setSendError(null);

      // Show the user's message immediately — don't wait for the LLM round-trip
      const optimistic: ChatMessage = {
        id: -Date.now(), // negative temp id, won't clash with DB auto-increment
        session_id: sessionId ?? '',
        role: 'user',
        content,
        created_at: new Date().toISOString(),
      };
      setPending([optimistic]);

      try {
        let sid = sessionId;
        if (!sid) {
          const session = await createSession();
          setSessionId(session.id);
          sid = session.id;
        }
        await sendMessage(sid, content);
        // Pull real messages (user + assistant) from server and clear optimistic
        setPending([]);
        await qc.invalidateQueries({ queryKey: ['messages', sid] });
      } catch (err) {
        console.error('chat send error', err);
        setSendError('Could not reach joecored. Make sure it is running and try again.');
        setPending([]);
      } finally {
        setIsSending(false);
      }
    },
    [sessionId, qc],
  );

  const startNewSession = useCallback(() => {
    setSessionId(null);
    setPending([]);
    setSendError(null);
    void qc.removeQueries({ queryKey: ['messages'] });
  }, [qc]);

  const fetched = messagesQ.data ?? [];
  const messages = useMemo(() => {
    if (pending.length === 0) return fetched;
    return [...fetched, ...pending];
  }, [fetched, pending]);

  return {
    sessionId,
    messages,
    isLoading: messagesQ.isLoading,
    isSending,
    sendError,
    send,
    startNewSession,
  };
}
