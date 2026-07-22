import { apiFetch } from './client';

export async function queryAI(query, sessionId = null) {
  const body = { query };
  if (sessionId) {
    body.session_id = sessionId;
  }
  return apiFetch('/api/v1/ai/query', {
    method: 'POST',
    body: JSON.stringify(body),
  });
}

// SSE Streaming setup
export function createQueryStream(query, sessionId = null, callbacks) {
  const params = new URLSearchParams({ query });
  if (sessionId) {
    params.set('session_id', sessionId);
  }

  const source = new EventSource(`/api/v1/ai/stream?${params}`);

  source.addEventListener('search_results', (e) => callbacks.onSearchResults?.(JSON.parse(e.data)));
  source.addEventListener('generation_start', (e) => callbacks.onGenerationStart?.(JSON.parse(e.data)));
  source.addEventListener('token', (e) => callbacks.onToken?.(JSON.parse(e.data)));
  source.addEventListener('generation_complete', (e) => callbacks.onGenerationComplete?.(JSON.parse(e.data)));
  
  source.addEventListener('complete', (e) => {
    callbacks.onComplete?.(JSON.parse(e.data));
    source.close();
  });
  
  source.addEventListener('error', (e) => {
    try {
      const data = JSON.parse(e.data);
      callbacks.onError?.(data);
    } catch {
      callbacks.onError?.({ code: 'CONNECTION_LOST', message: 'Connection to server lost', retry: true });
    }
    source.close();
  });

  return source;
}
