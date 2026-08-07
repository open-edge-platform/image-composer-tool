import React from 'react';
import styles from './ErrorBanner.module.css';

export function ErrorBanner({ error, onRetry }) {
  if (!error) return null;

  return (
    <div className={styles.banner}>
      <div className={styles.content}>
        <h4 className={styles.title}>Error: {error.code || 'UNKNOWN_ERROR'}</h4>
        <p className={styles.message}>{error.message || 'An unexpected error occurred.'}</p>
      </div>
      {onRetry && (error.retry !== false) && (
        <button onClick={onRetry} className={styles.retryButton}>
          Retry
        </button>
      )}
    </div>
  );
}
