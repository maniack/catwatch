/* eslint-env browser */
/* global React, window */
import Avatar from './Avatar.jsx';

export default function Navbar({ theme, setTheme, user }) {
  const { useState, useEffect } = React;
  const [canGoBack, setCanGoBack] = useState(false);

  useEffect(() => {
    const update = () => {
      try {
        const h = window.location.hash || '#/';
        const base = (h.split('?')[0] || '#/');
        setCanGoBack(!(base === '#/' || base === '#'));
      } catch(_) { setCanGoBack(false); }
    };
    update();
    window.addEventListener('hashchange', update);
    return () => window.removeEventListener('hashchange', update);
  }, []);

  return (
    <nav className="navbar sticky-top bg-body-tertiary border-bottom">
      <div className="container-fluid px-3 d-flex align-items-center">
        {canGoBack ? (
          <button className="btn btn-link text-decoration-none me-2 d-flex align-items-center" type="button" onClick={()=> window.history.back()}>
            <i className="fa-solid fa-chevron-left me-1"></i>
            <span>Back</span>
          </button>
        ) : (
          <div className="dropdown me-2">
            <button className="btn btn-link text-decoration-none d-flex align-items-center" type="button" data-bs-toggle="dropdown">
              <i className="fa-solid fa-ellipsis"></i>
            </button>
            <div className="dropdown-menu dropdown-menu-start p-2">
              <button className="dropdown-item" type="button" onClick={()=> setTheme(theme === 'dark' ? 'light' : 'dark')}>
                <i className={theme === 'dark' ? 'fa-solid fa-sun me-2' : 'fa-solid fa-moon me-2'}></i>
                {theme === 'dark' ? 'Light theme' : 'Dark theme'}
              </button>
              {user && (
                <>
                  <div className="dropdown-divider"></div>
                  <a href="#/cat/new" className="dropdown-item"><i className="fa-solid fa-plus me-2"></i>Add cat</a>
                  <a href="#/upcoming" className="dropdown-item"><i className="fa-solid fa-calendar-day me-2"></i>Upcoming</a>
                  <a href="#/me" className="dropdown-item"><i className="fa-solid fa-user me-2"></i>My profile</a>
                </>
              )}
            </div>
          </div>
        )}
        
        <a className="navbar-brand fw-bold d-flex align-items-center mx-auto" href="#/">
          <i className="fa-solid fa-cat me-2"></i>
          <span>CatWatch</span>
        </a>

        <div className="d-flex align-items-center">
          {user ? (
            <a href="#/me" className="d-inline-flex align-items-center justify-content-center ms-1">
              <Avatar user={user} size={32} className="border" />
            </a>
          ) : (
            <a href="#/signin" className="btn btn-link text-decoration-none"><i className="fa-solid fa-user"></i></a>
          )}
        </div>
      </div>
    </nav>
  );
}
