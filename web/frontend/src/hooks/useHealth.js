import { useState, useEffect } from 'react';
import { getHealth } from '../api/engine';

export function useHealth(pollIntervalMs = 5000) {
  const [health, setHealth] = useState({ status: 'initializing', message: '' });
  const [error, setError] = useState(null);

  useEffect(() => {
    let mounted = true;

    async function checkHealth() {
      try {
        const data = await getHealth();
        if (mounted) {
          setHealth(data);
          setError(null);
        }
      } catch (err) {
        if (mounted) {
          setHealth({ status: 'error', message: err.message });
          setError(err);
        }
      }
    }

    // Initial check
    checkHealth();

    // Poll interval
    const intervalId = setInterval(checkHealth, pollIntervalMs);

    return () => {
      mounted = false;
      clearInterval(intervalId);
    };
  }, [pollIntervalMs]);

  return { health, error };
}
