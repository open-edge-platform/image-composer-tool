import React, { createContext, useContext, useState, useRef, useCallback } from 'react';
import { createQueryStream } from '../api/ai';

const ChatContext = createContext(null);

export function ChatProvider({ children }) {
  const [messages, setMessages] = useState([]);
  const [error, setError] = useState(null);
  const [isStreaming, setIsStreaming] = useState(false);
  const [streamBuffer, setStreamBuffer] = useState('');
  const [activeSearchResults, setActiveSearchResults] = useState([]);
  const activeSearchResultsRef = useRef([]);

  // SSE stream reference — lives at app level so tab switches don't kill it
  const streamRef = useRef(null);

  const startStream = useCallback((query, sessionId, callbacks) => {
    // Abort any existing stream before starting a new one
    if (streamRef.current) {
      streamRef.current.close();
      streamRef.current = null;
    }
    const source = createQueryStream(query, sessionId, callbacks);
    streamRef.current = source;
  }, []);

  const stopStream = useCallback(() => {
    if (streamRef.current) {
      streamRef.current.close();
      streamRef.current = null;
    }
  }, []);

  const resetChat = useCallback(() => {
    stopStream();
    setMessages([]);
    setError(null);
    setIsStreaming(false);
    setStreamBuffer('');
    setActiveSearchResults([]);
    activeSearchResultsRef.current = [];
  }, [stopStream]);

  return (
    <ChatContext.Provider value={{
      messages, setMessages,
      error, setError,
      isStreaming, setIsStreaming,
      streamBuffer, setStreamBuffer,
      activeSearchResults, setActiveSearchResults,
      activeSearchResultsRef,
      startStream, stopStream,
      resetChat,
    }}>
      {children}
    </ChatContext.Provider>
  );
}

export const useChatContext = () => useContext(ChatContext);
