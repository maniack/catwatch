/* eslint-env browser */
/* global React, window */
import useRoute from '../hooks/useRoute.js';
import useAppInit from '../hooks/useAppInit.js';
import Navbar from './Navbar.jsx';
import MainView from '../views/MainView.jsx';
import CatView from '../views/CatView.jsx';
import CatEditView from '../views/CatEditView.jsx';
import UpcomingView from '../views/UpcomingView.jsx';
import SignInView from '../views/SignInView.jsx';
import UserView from '../views/UserView.jsx';
import api from '../api/api.js';

export default function App() {
  const { useEffect } = React;
  const route = useRoute();
  const { config, user, setUser, theme, setTheme } = useAppInit();

  useEffect(() => {
    if (!user) {
      api.get('/api/user').then(setUser).catch(() => {});
    }
  }, [route]);

  let content = null;
  if (route.startsWith('#/cat/view/')) {
    const id = route.replace('#/cat/view/', '').split('?')[0].replace(/\/$/, '');
    content = <CatView key={`view-${id}`} catId={id} user={user}/>;
  } else if (route.startsWith('#/cat/edit/')) {
    const id = route.replace('#/cat/edit/', '').split('?')[0].replace(/\/$/, '');
    content = <CatEditView key={`edit-${id}`} catId={id} user={user}/>;
  } else if (route === '#/cat/new') {
    content = <CatEditView key="new" user={user}/>;
  } else if (route === '#/upcoming') {
    content = <UpcomingView key="upcoming" user={user}/>;
  } else if (route === '#/me') {
    content = <UserView key="me" user={user}/>;
  } else if (route === '#/signin') {
    content = <SignInView key="signin" config={config} />;
  } else {
    content = <MainView key="main" user={user}/>;
  }

  return (
    <div>
      <Navbar theme={theme} setTheme={setTheme} user={user} />
      <div className="container-fluid py-3">
        {content}
      </div>
    </div>
  );
}
