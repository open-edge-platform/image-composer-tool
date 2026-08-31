import { apiFetch } from './client.js';

export async function validateTemplate(yaml) {
  return apiFetch('/api/v1/templates/validate', {
    method: 'POST',
    body: JSON.stringify({ yaml }),
  });
}
