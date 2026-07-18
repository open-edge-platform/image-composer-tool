import { useState, useEffect } from 'react';
import { getEngineStats } from '../api/engine';

/**
 * Fetches engine stats once on mount to get provider and model info.
 * This is NOT a poller — provider/model info doesn't change at runtime.
 * Returns null while loading, then the stats object.
 *
 * Stats object shape (from GET /api/v1/engine/stats):
 * {
 *   provider: "ollama" | "openai",
 *   chat_model: "llama3.2" | "gpt-4o-mini",
 *   embedding_model: "nomic-embed-text" | "text-embedding-3-small",
 *   initialized: true,
 *   template_count: 54,
 *   ...
 * }
 */
export function useEngineStats() {
  const [stats, setStats] = useState(null);

  useEffect(() => {
    let mounted = true;

    getEngineStats()
      .then((data) => {
        if (mounted) setStats(data);
      })
      .catch(() => {
        // Silently fail — the StatusIndicator/useHealth handles
        // connection errors. This hook is purely for display labels.
      });

    return () => {
      mounted = false;
    };
  }, []);

  return stats;
}
