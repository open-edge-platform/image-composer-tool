import React, { useState, useRef, useEffect } from 'react';
import styles from './ChatInput.module.css';

export function ChatInput({ onSubmit, isStreaming }) {
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
        <button 
          className={styles.sendButton}
          onClick={handleSubmit}
          disabled={!query.trim() || isStreaming}
        >
          {isStreaming ? 'Generating...' : 'Send'}
        </button>
      </div>
    </div>
  );
}
