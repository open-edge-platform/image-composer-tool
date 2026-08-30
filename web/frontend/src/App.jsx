import React from 'react';
import { Outlet } from 'react-router-dom';
import { Sidebar } from './components/Sidebar/Sidebar';
import { SessionProvider } from './hooks/SessionContext';
import { ChatProvider } from './hooks/ChatContext';
import { EditorProvider } from './hooks/EditorContext';
import styles from './App.module.css';

function App() {
  return (
    <SessionProvider>
      <ChatProvider>
        <EditorProvider>
          <div className={styles.app}>
            <Sidebar />
            <main className={styles.main}>
              {/* Outlet renders the matched child route component */}
              <Outlet />
            </main>
          </div>
        </EditorProvider>
      </ChatProvider>
    </SessionProvider>
  );
}

export default App;
