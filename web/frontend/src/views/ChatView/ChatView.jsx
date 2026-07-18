import React, { useRef, useEffect } from 'react';
import styles from './ChatView.module.css';
import { useChat } from '../../hooks/useChat';
import { useEngineStats } from '../../hooks/useEngineStats';
import { ChatInput } from '../../components/ChatInput/ChatInput';
import { MessageBubble } from '../../components/MessageBubble/MessageBubble';
import { ErrorBanner } from '../../components/ErrorBanner/ErrorBanner';
import { StreamingYaml } from '../../components/StreamingYaml/StreamingYaml';
import { SearchResultCard } from '../../components/SearchResultCard/SearchResultCard';
import searchSvgUrl from '../../assets/search.svg';

export function ChatView() {
  const {
    messages,
    error,
    isStreaming,
    streamBuffer,
    activeSearchResults,
    handleSubmit: submitChat,
    clearError
  } = useChat();
  const engineStats = useEngineStats();

  const messageAreaRef = useRef(null);
  const messageEndRef = useRef(null);

  const scrollToBottom = React.useCallback((force = false) => {
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
  }, []);

  useEffect(() => {
    scrollToBottom(false);
  }, [messages, streamBuffer, activeSearchResults, scrollToBottom]);

  const handleSubmit = (query) => {
    submitChat(query);
    setTimeout(() => scrollToBottom(true), 50);
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
              <div className={styles.searchingAnimationWrapper}>
                <img src={searchSvgUrl} alt="Searching" style={{ width: '40px', height: '40px' }} />
                <span>Searching similar templates in cache...</span>
              </div>
            ) : null}

            {activeSearchResults.length > 0 && (
              <div className={styles.searchResultWrapper}>
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
        <ErrorBanner error={error} onRetry={clearError} />
        <ChatInput onSubmit={handleSubmit} isStreaming={isStreaming} providerInfo={engineStats} />
      </div>
    </div>
  );
}

