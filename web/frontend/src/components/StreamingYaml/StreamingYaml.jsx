import React from 'react';
import styles from './StreamingYaml.module.css';

export function StreamingYaml({ yaml, isStreaming }) {
  if (!yaml && !isStreaming) return null;

  return (
    <div className={styles.container}>
      <code className={styles.code}>
        {yaml}
        {isStreaming && <span className={styles.cursor} />}
      </code>
    </div>
  );
}
