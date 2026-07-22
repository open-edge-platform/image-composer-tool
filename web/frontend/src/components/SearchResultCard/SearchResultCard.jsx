import React from 'react';
import styles from './SearchResultCard.module.css';

export function SearchResultCard({ result }) {
  if (!result || !result.template) return null;

  const { template, score, semantic_score, keyword_score, package_score } = result;

  return (
    <div className={styles.card}>
      <div className={styles.header}>
        <span>{template.file_name}</span>
        <span className={styles.score}>{(score * 100).toFixed(1)}%</span>
      </div>
      <div className={styles.details}>
        <span title="Semantic Score">Sem: {(semantic_score * 100).toFixed(0)}</span>
        <span title="Keyword Score">Key: {(keyword_score * 100).toFixed(0)}</span>
        <span title="Package Score">Pkg: {(package_score * 100).toFixed(0)}</span>
      </div>
    </div>
  );
}
