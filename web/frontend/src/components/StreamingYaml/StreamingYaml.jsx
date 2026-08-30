import React from 'react';
import styles from './StreamingYaml.module.css';

export function StreamingYaml({ yaml, isStreaming, onOpenInEditor }) {
  if (!yaml && !isStreaming) return null;

  return (
    <div className={styles.container}>
      <code className={styles.code}>
        {yaml}
        {isStreaming && <span className={styles.cursor} />}
      </code>
      {!isStreaming && onOpenInEditor && (
        <div className={styles.actionWrapper}>
          <button className={styles.actionBtn} onClick={onOpenInEditor}>
            Open in Editor
          </button>
        </div>
      )}
    </div>
  );
}
