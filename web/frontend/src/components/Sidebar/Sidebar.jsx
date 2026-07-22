import React from 'react';
import { NavLink } from 'react-router-dom';
import { StatusIndicator } from '../StatusIndicator/StatusIndicator';
import styles from './Sidebar.module.css';
import logoUrl from '../../assets/logo.png';

export function Sidebar() {
  const getNavClass = ({ isActive }) =>
    isActive ? `${styles.navLink} ${styles.navLinkActive}` : styles.navLink;

  return (
    <aside className={styles.sidebar}>
      <div className={styles.brand}>
        <img src={logoUrl} alt="Intel Logo" className={styles.logoImage} />
        <h1 className={styles.brandTitle}>Image Composer</h1>
      </div>

      <nav className={styles.nav}>
        <NavLink to="/" className={getNavClass} end>Chat</NavLink>
        <NavLink to="/editor" className={getNavClass}>Editor</NavLink>
        <NavLink to="/templates" className={getNavClass}>Library</NavLink>
        <NavLink to="/builds" className={getNavClass}>Builds</NavLink>
      </nav>

      <div className={styles.footer}>
        <StatusIndicator />
      </div>
    </aside>
  );
}
