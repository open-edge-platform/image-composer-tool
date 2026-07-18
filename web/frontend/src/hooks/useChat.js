import { useState, useRef, useEffect } from 'react';
import { useSSE } from './useSSE';
import { useSession } from './SessionContext';

export function useChat() {
  const [messages, setMessages] = useState([]);
  const [error, setError] = useState(null);
  const [isStreaming, setIsStreaming] = useState(false);
  
  // Active stream state
  const [streamBuffer, setStreamBuffer] = useState('');
  const [activeSearchResults, setActiveSearchResults] = useState([]);
  const activeSearchResultsRef = useRef([]);

  const { startStream } = useSSE();
  const { sessionId, ensureSession, clearSession } = useSession();

  // Clear messages if session is cleared from elsewhere (e.g. Sidebar)
  useEffect(() => {
    if (sessionId === null) {
      setMessages([]);
      setError(null);
      setStreamBuffer('');
      setActiveSearchResults([]);
      activeSearchResultsRef.current = [];
    }
  }, [sessionId]);

  const resetChat = () => {
    clearSession();
  };

  const handleSubmit = async (query) => {
    setError(null);
    setStreamBuffer('');
    setActiveSearchResults([]);
    activeSearchResultsRef.current = [];
    setIsStreaming(true);

    // Add user message to history
    setMessages((prev) => [...prev, { role: 'user', content: query }]);

    try {
      const sessionId = await ensureSession();
      
      startStream(query, sessionId, {
        onSearchResults: (data) => {
          const results = data.results || [];
          setActiveSearchResults(results);
          activeSearchResultsRef.current = results;
        },
        onToken: (data) => {
          setStreamBuffer((prev) => prev + data.content);
        },
        onError: (err) => {
          setError(err);
          setIsStreaming(false);
        },
        onComplete: (data) => {
          const finalResults = activeSearchResultsRef.current;
          setMessages((prev) => [
            ...prev, 
            { 
              role: 'assistant', 
              yaml: data.yaml,
              searchResults: finalResults,
              changes: data.changes || [],
              isRefinement: prev.length > 1
            }
          ]);
          setStreamBuffer('');
          setActiveSearchResults([]);
          activeSearchResultsRef.current = [];
          setIsStreaming(false);
        }
      });
    } catch (err) {
      setError({ message: 'Failed to create session. Please try again.' });
      setIsStreaming(false);
    }
  };

  const clearError = () => setError(null);

  return {
    messages,
    error,
    isStreaming,
    streamBuffer,
    activeSearchResults,
    handleSubmit,
    clearError,
    resetChat
  };
}
