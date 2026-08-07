import React, { useState, useRef, useEffect } from 'react';
import styles from './ChatInput.module.css';

export function ChatInput({ onSubmit, isStreaming, providerInfo }) {
  const [query, setQuery] = useState('');
  const textareaRef = useRef(null);

  // Auto-resize textarea
  useEffect(() => {
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto';
      textareaRef.current.style.height = `${textareaRef.current.scrollHeight}px`;
    }
  }, [query]);

  const handleKeyDown = (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSubmit();
    }
  };

  const handleSubmit = () => {
    if (query.trim() && !isStreaming) {
      onSubmit(query.trim());
      setQuery('');
    }
  };

  // Derive display labels from backend stats
  const providerName = providerInfo?.provider
    ? providerInfo.provider.charAt(0).toUpperCase() + providerInfo.provider.slice(1)
    : 'Loading...';
  const chatModelLabel = providerInfo?.chat_model ?? '...';
  const embedModelLabel = providerInfo?.embedding_model ?? '...';

  return (
    <div className={styles.container}>
      <textarea
        ref={textareaRef}
        className={styles.textarea}
        placeholder="Ask to create or modify an OS template..."
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={handleKeyDown}
        disabled={isStreaming}
        rows={1}
      />
      <div className={styles.footer}>
        <div className={styles.modelSelectors}>
          <div className={styles.modelSelector} title="Chat Model">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"></path></svg>
            {providerInfo ? `${providerName} (${chatModelLabel})` : 'Loading...'}
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polyline points="18 15 12 9 6 15"></polyline></svg>
          </div>
          <div className={styles.modelSelector} title="Embedding Model">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 2v20M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"></path></svg>
            {providerInfo ? `${providerName} (${embedModelLabel})` : 'Loading...'}
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polyline points="18 15 12 9 6 15"></polyline></svg>
          </div>
        </div>
        <button 
          className={`${styles.sendButton} ${isStreaming ? styles.generating : ''}`}
          onClick={handleSubmit}
          disabled={!query.trim() || isStreaming}
        >
          {isStreaming ? (
            <span className={styles.generatingState}>
              Generating<span className={styles.dot1}>.</span><span className={styles.dot2}>.</span><span className={styles.dot3}>.</span>
            </span>
          ) : 'Send'}
        </button>
      </div>
    </div>
  );
}
