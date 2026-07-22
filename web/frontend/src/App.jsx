import React from 'react';
import { Outlet } from 'react-router-dom';
import { Sidebar } from './components/Sidebar/Sidebar';
import styles from './App.module.css';

function App() {
  return (
    <div className={styles.app}>
      <Sidebar />
      <main className={styles.main}>
        {/* Outlet renders the matched child route component */}
        <Outlet />
      </main>
    </div>
  );
}

export default App;
