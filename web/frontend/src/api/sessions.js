import { apiFetch } from './client.js';

/**
 * Creates a new AI conversation session.
 * @returns {Promise<Object>} The new session object
 */
export async function createSession() {
  return apiFetch('/api/v1/sessions', { method: 'POST' });
}

/**
 * Retrieves an existing session by ID.
 * @param {string} id - The session ID
 * @returns {Promise<Object>} The session object
 */
export async function getSession(id) {
  return apiFetch(`/api/v1/sessions/${id}`);
}

/**
 * Deletes a session by ID.
 * @param {string} id - The session ID
 * @returns {Promise<void>}
 */
export async function deleteSession(id) {
  return apiFetch(`/api/v1/sessions/${id}`, { method: 'DELETE' });
}
