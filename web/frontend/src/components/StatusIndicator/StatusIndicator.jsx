import React from 'react';
import { useHealth } from '../../hooks/useHealth';
import styles from './StatusIndicator.module.css';

export function StatusIndicator() {
  const { health } = useHealth(5000); // poll every 5s

  let statusClass = styles.status_error;
  let displayLabel = 'Disconnected';

  if (health.status === 'ok') {
    statusClass = styles.status_ok;
    displayLabel = 'Engine Ready';
  } else if (health.status === 'initializing') {
    statusClass = styles.status_initializing;
    displayLabel = 'Initializing...';
  } else if (health.status === 'error') {
    statusClass = styles.status_error;
    displayLabel = 'Engine Error';
  }

  return (
    <div className={`${styles.container} ${statusClass}`} title={health.message || displayLabel}>
      <div className={styles.dot} />
      <span className={styles.label}>{displayLabel}</span>
    </div>
  );
}
