import { useRef, useCallback, useEffect } from 'react';
import { createQueryStream } from '../api/ai';

export function useSSE() {
  const streamRef = useRef(null);

  const startStream = useCallback((query, sessionId, callbacks) => {
    // Abort any existing stream
    if (streamRef.current) {
      streamRef.current.close();
      streamRef.current = null;
    }

    const source = createQueryStream(query, sessionId, callbacks);
    streamRef.current = source;

    return () => {
      source.close();
      if (streamRef.current === source) {
        streamRef.current = null;
      }
    };
  }, []);

  const stopStream = useCallback(() => {
    if (streamRef.current) {
      streamRef.current.close();
      streamRef.current = null;
    }
  }, []);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (streamRef.current) {
        streamRef.current.close();
      }
    };
  }, []);

  return { startStream, stopStream };
}
