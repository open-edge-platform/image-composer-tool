import { apiFetch } from './client.js';

export async function getHealth(options = {}) {
  return apiFetch('/api/v1/health', options);
}

export async function getEngineStats(options = {}) {
  return apiFetch('/api/v1/engine/stats', options);
}
