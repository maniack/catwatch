/* eslint-env browser */
/* global React, document, localStorage, window */
import api from '../api/api.js';
export default function useAppInit() {
  const { useState, useEffect } = React;
  const [config, setConfig] = useState({ googleEnabled: false, oidcEnabled: false, devLogin: false });
  const [user, setUser] = useState(null);
  const [theme, setTheme] = useState(localStorage.getItem('theme') || 'dark');

  // Theme
  useEffect(() => {
    document.documentElement.setAttribute('data-bs-theme', theme);
    localStorage.setItem('theme', theme);
  }, [theme]);

  // Load config and user
  useEffect(() => {
    fetch('/api/config').then(r => r.json()).then(setConfig).catch(() => {});
  }, []);

  useEffect(() => {
    api.get('/api/user').then(setUser).catch(() => setUser(null));
  }, []);

  return { config, user, setUser, theme, setTheme };
}
