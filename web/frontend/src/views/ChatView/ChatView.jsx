import React, { useState, useRef, useEffect } from 'react';
import styles from './ChatView.module.css';
import { useSSE } from '../../hooks/useSSE';
import { ChatInput } from '../../components/ChatInput/ChatInput';
import { MessageBubble } from '../../components/MessageBubble/MessageBubble';
import { ErrorBanner } from '../../components/ErrorBanner/ErrorBanner';
import { StreamingYaml } from '../../components/StreamingYaml/StreamingYaml';
import { SearchResultCard } from '../../components/SearchResultCard/SearchResultCard';
import searchSvgUrl from '../../assets/search.svg';

export function ChatView() {
  const [messages, setMessages] = useState([]);
  const [error, setError] = useState(null);
  const [isStreaming, setIsStreaming] = useState(false);
  
  // Active stream state
  const [streamBuffer, setStreamBuffer] = useState('');
  const [activeSearchResults, setActiveSearchResults] = useState([]);
  const activeSearchResultsRef = useRef([]);

  const { startStream } = useSSE();
  const messageAreaRef = useRef(null);
  const messageEndRef = useRef(null);

  const scrollToBottom = (force = false) => {
    if (!messageAreaRef.current) return;
    
    if (force) {
      messageEndRef.current?.scrollIntoView({ behavior: 'smooth' });
      return;
    }

    const { scrollTop, scrollHeight, clientHeight } = messageAreaRef.current;
    // Tolerance of 100 pixels to count as "at the bottom"
    const isScrolledUp = scrollHeight - scrollTop - clientHeight > 100;
    
    if (!isScrolledUp) {
      messageEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    }
  };

  useEffect(() => {
    scrollToBottom(false);
  }, [messages, streamBuffer, activeSearchResults]);

  const handleSubmit = (query) => {
    setError(null);
    setStreamBuffer('');
    setActiveSearchResults([]);
    activeSearchResultsRef.current = [];
    setIsStreaming(true);

    // Add user message to history
    setMessages((prev) => [...prev, { role: 'user', content: query }]);
    setTimeout(() => scrollToBottom(true), 50);

    startStream(query, null, {
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
            searchResults: finalResults 
          }
        ]);
        setStreamBuffer('');
        setActiveSearchResults([]);
        activeSearchResultsRef.current = [];
        setIsStreaming(false);
      }
    });
  };

  return (
    <div className={styles.container}>
      
      <div className={styles.messageArea} ref={messageAreaRef}>
        {messages.length === 0 ? (
          <div className={styles.emptyState}>
            <h2>Image Composer</h2>
            <p>Describe the OS image template you want to build.</p>
          </div>
        ) : (
          messages.map((msg, idx) => (
            <MessageBubble key={idx} message={msg} />
          ))
        )}

        {/* Active streaming area */}
        {isStreaming && (
          <div style={{ marginTop: '16px' }}>
            {activeSearchResults.length === 0 && streamBuffer === '' ? (
              <div style={{ display: 'flex', alignItems: 'center', gap: '16px', padding: '16px', color: 'var(--color-text-secondary)', fontSize: '1.1rem' }}>
                <img src={searchSvgUrl} alt="Searching" style={{ width: '40px', height: '40px' }} />
                <span>Searching similar templates in cache...</span>
              </div>
            ) : null}

            {activeSearchResults.length > 0 && (
              <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap', margin: '12px 0' }}>
                {activeSearchResults.map((res, i) => (
                  <SearchResultCard key={i} result={res} />
                ))}
              </div>
            )}
            <StreamingYaml yaml={streamBuffer} isStreaming={true} />
          </div>
        )}
        <div ref={messageEndRef} />
      </div>

      <div className={styles.inputArea}>
        <ErrorBanner error={error} onRetry={() => setError(null)} />
        <ChatInput onSubmit={handleSubmit} isStreaming={isStreaming} />
      </div>
    </div>
  );
}

