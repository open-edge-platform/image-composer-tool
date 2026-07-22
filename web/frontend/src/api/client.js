// src/api/client.js
// Custom error class mapped to the API error response envelope
export class ApiError extends Error {
  constructor(code, message, details) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.details = details;
  }
}

// The proxy handles routing /api to the backend in dev, so base is empty
const API_BASE = ''; 

export async function apiFetch(path, options = {}) {
  const res = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options.headers,
    },
  });

  if (!res.ok) {
    let body;
    try {
      body = await res.json();
    } catch {
      throw new Error(`Network error: ${res.statusText}`);
    }
    
    // Check if it matches our standard API error envelope
    if (body && body.error) {
      throw new ApiError(body.error.code, body.error.message, body.error.details);
    }
    
    throw new Error(`API Error ${res.status}: ${res.statusText}`);
  }

  return res.json();
}
