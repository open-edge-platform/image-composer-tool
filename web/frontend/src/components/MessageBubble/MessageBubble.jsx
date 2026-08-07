import React from 'react';
import styles from './MessageBubble.module.css';
import { StreamingYaml } from '../StreamingYaml/StreamingYaml';
import { SearchResultCard } from '../SearchResultCard/SearchResultCard';

export function MessageBubble({ message }) {
  const isUser = message.role === 'user';
  
  return (
    <div className={`${styles.bubbleContainer} ${isUser ? styles.user : styles.assistant}`}>
      <div className={styles.bubble}>
        {message.isRefinement && (
          <span className={styles.refinementBadge}>Refinement</span>
        )}

        {message.content && <p className={styles.text}>{message.content}</p>}
        
        {!isUser && message.searchResults && message.searchResults.length > 0 && (
          <div className={styles.searchResultWrapper}>
            {message.searchResults.map((res, i) => (
              <SearchResultCard key={i} result={res} />
            ))}
          </div>
        )}

        {!isUser && message.yaml && (
          <StreamingYaml yaml={message.yaml} isStreaming={false} />
        )}
      </div>
    </div>
  );
}
