import { useEffect } from 'react';
import { useSession } from './SessionContext';
import { useChatContext } from './ChatContext';
import { useEditor } from './EditorContext';

export function useChat() {
  const {
    messages, setMessages,
    error, setError,
    isStreaming, setIsStreaming,
    streamBuffer, setStreamBuffer,
    activeSearchResults, setActiveSearchResults,
    activeSearchResultsRef,
    startStream,
    resetChat: contextReset,
  } = useChatContext();

  const { sessionId, ensureSession, clearSession } = useSession();
  const { setEditorValue } = useEditor();

  // Clear messages if session is cleared from elsewhere (e.g. Sidebar)
  useEffect(() => {
    if (sessionId === null) {
      contextReset();
    }
  }, [sessionId]); // eslint-disable-line react-hooks/exhaustive-deps

  const resetChat = () => {
    clearSession();
    // contextReset() will be called by the useEffect above when sessionId becomes null
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
      
      let currentBuffer = '';

      startStream(query, sessionId, {
        onSearchResults: (data) => {
          const results = data.results || [];
          setActiveSearchResults(results);
          activeSearchResultsRef.current = results;
        },
        onToken: (data) => {
          currentBuffer += data.content;
          setStreamBuffer(currentBuffer);
          setEditorValue(currentBuffer);
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
          // Auto-sync: push the latest generated YAML into the Editor context
          if (data.yaml) {
            setEditorValue(data.yaml);
          }
          setStreamBuffer('');
          setActiveSearchResults([]);
          activeSearchResultsRef.current = [];
          setIsStreaming(false);
        }
      });
    } catch (err) {
      setError({ message: err.message || 'Failed to create session. Please try again.' });
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
