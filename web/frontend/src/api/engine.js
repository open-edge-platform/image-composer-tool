import { apiFetch } from './client';

export async function getHealth() {
  return apiFetch('/api/v1/health');
}

export async function getEngineStats() {
  return apiFetch('/api/v1/engine/stats');
}
