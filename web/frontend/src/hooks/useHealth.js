import { useState, useEffect } from 'react';
import { getHealth } from '../api/engine';

export function useHealth(pollIntervalMs = 5000) {
  const [health, setHealth] = useState({ status: 'initializing', message: '' });
  const [error, setError] = useState(null);

  useEffect(() => {
    let mounted = true;
    let timeoutId;
    const controller = new AbortController();

    async function checkHealth() {
      try {
        const data = await getHealth({ signal: controller.signal });
        if (mounted) {
          setHealth(data);
          setError(null);
        }
      } catch (err) {
        if (err.name === 'AbortError') return;
        if (mounted) {
          setHealth({ status: 'error', message: err.message });
          setError(err);
        }
      } finally {
        if (mounted) {
          timeoutId = setTimeout(checkHealth, pollIntervalMs);
        }
      }
    }

    // Initial check
    checkHealth();

    return () => {
      mounted = false;
      controller.abort();
      clearTimeout(timeoutId);
    };
  }, [pollIntervalMs]);

  return { health, error };
}
