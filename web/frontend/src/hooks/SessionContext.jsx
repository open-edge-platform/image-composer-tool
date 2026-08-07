import React, { createContext, useContext, useState, useCallback } from 'react';
import { createSession, deleteSession } from '../api/sessions.js';

const SessionContext = createContext(null);

export function SessionProvider({ children }) {
  const [sessionId, setSessionId] = useState(null);
  const [isCreating, setIsCreating] = useState(false);

  // Ensures we have an active session, creating one if necessary
  const ensureSession = useCallback(async () => {
    if (sessionId) return sessionId;
    
    setIsCreating(true);
    try {
      const session = await createSession();
      setSessionId(session.id);
      return session.id;
    } catch (error) {
      console.error('Failed to create session:', error);
      throw error;
    } finally {
      setIsCreating(false);
    }
  }, [sessionId]);

  // Clears the current session and deletes it from the backend
  const clearSession = useCallback(() => {
    if (sessionId) {
      // Fire and forget delete
      deleteSession(sessionId).catch(err => {
        console.error('Failed to delete session on backend:', err);
      });
    }
    setSessionId(null);
  }, [sessionId]);

  return (
    <SessionContext.Provider value={{ sessionId, ensureSession, clearSession, isCreating }}>
      {children}
    </SessionContext.Provider>
  );
}

export const useSession = () => useContext(SessionContext);
