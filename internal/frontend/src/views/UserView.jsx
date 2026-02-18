/* eslint-env browser */
/* global React, window, alert */
import api from '../api/api.js';
import Avatar from '../app/Avatar.jsx';
import CatCard from '../components/CatCard.jsx';

export default function UserView({ user }) {
  const { useState, useEffect } = React;
  const [likedCats, setLikedCats] = useState([]);
  const [auditLogs, setAuditLogs] = useState([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!user) return;
    setLoading(true);
    
    Promise.all([
      api.get('/api/user/likes'),
      api.get('/api/user/audit')
    ])
      .then(([likes, logs]) => {
        setLikedCats(likes || []);
        setAuditLogs(logs || []);
        setLoading(false);
      })
      .catch(err => {
        console.error(err);
        setLoading(false);
      });
  }, [user]);

  const handleLogout = async () => {
    try {
      await api.post('/api/auth/logout', {});
      localStorage.removeItem('token');
      window.location.hash = '#/';
      window.location.reload();
    } catch (err) {
      alert('Logout failed: ' + err.message);
    }
  };

  const removeFromList = (id) => {
    setLikedCats(prev => prev.filter(c => c.id !== id));
  };

  if (!user) {
    return (
      <div className="text-center py-5">
        <p className="lead text-secondary">Please sign in to view your profile.</p>
        <a href="#/signin" className="btn btn-primary rounded-pill px-4">Sign In</a>
      </div>
    );
  }

  return (
    <div className="container py-4" style={{ maxWidth: '900px' }}>
      <div className="card border-0 bg-body-tertiary shadow-sm rounded-4 p-4 mb-5">
        <div className="d-flex align-items-center flex-wrap gap-4">
          <Avatar user={user} size={100} className="border shadow-sm" />
          <div className="flex-grow-1">
            <h2 className="fw-bold mb-1">{user.name || 'Volunteer'}</h2>
            <p className="text-secondary mb-3">{user.email}</p>
            <div className="d-flex gap-2">
              <button className="btn btn-outline-danger btn-sm rounded-pill px-3" onClick={handleLogout}>
                <i className="fa-solid fa-right-from-bracket me-2"></i>
                Sign Out
              </button>
            </div>
          </div>
        </div>
      </div>

      <div className="d-flex justify-content-between align-items-center mb-4">
        <h4 className="fw-bold mb-0">
          <i className="fa-solid fa-heart text-danger me-2"></i>
          Favorite Cats
        </h4>
        <span className="badge bg-secondary rounded-pill">{likedCats.length}</span>
      </div>

      {loading ? (
        <div className="text-center py-5">
          <div className="spinner-border text-primary"></div>
        </div>
      ) : likedCats.length === 0 ? (
        <div className="text-center py-5 bg-body-tertiary rounded-4 border border-dashed">
          <i className="fa-regular fa-heart fa-3x mb-3 opacity-25"></i>
          <p className="text-secondary lead">You haven't liked any cats yet.</p>
          <a href="#/" className="btn btn-outline-primary rounded-pill px-4">Explore Cats</a>
        </div>
      ) : (
        <div className="row row-cols-1 row-cols-sm-2 row-cols-md-3 g-4">
          {likedCats.map(cat => (
            <div key={cat.id} className="col">
              <CatCard cat={cat} user={user} onUnliked={removeFromList} />
            </div>
          ))}
        </div>
      )}

      <div className="d-flex justify-content-between align-items-center mt-5 mb-4">
        <h4 className="fw-bold mb-0">
          <i className="fa-solid fa-clock-rotate-left text-primary me-2"></i>
          Activity Log
        </h4>
        <span className="badge bg-secondary rounded-pill">{auditLogs.length}</span>
      </div>

      <div className="card border-0 bg-body-tertiary shadow-sm rounded-4 overflow-hidden mb-5">
        <div className="table-responsive">
          <table className="table table-hover mb-0">
            <thead className="table-light">
              <tr>
                <th className="border-0 ps-4 small text-uppercase fw-bold text-secondary">Time</th>
                <th className="border-0 small text-uppercase fw-bold text-secondary">Action</th>
                <th className="border-0 text-end pe-4 small text-uppercase fw-bold text-secondary">Status</th>
              </tr>
            </thead>
            <tbody className="border-top-0">
              {auditLogs.map(log => (
                <tr key={log.id}>
                  <td className="ps-4 text-secondary small align-middle">
                    {new Date(log.ts).toLocaleString([], { dateStyle: 'short', timeStyle: 'short' })}
                  </td>
                  <td className="align-middle">
                    <div className="fw-medium text-capitalize mb-0">
                      {log.method.toLowerCase() === 'post' ? 'Created' : 
                       log.method.toLowerCase() === 'put' ? 'Updated' : 
                       log.method.toLowerCase() === 'delete' ? 'Deleted' : log.method} {log.target_type}
                    </div>
                    <div className="text-secondary x-small font-monospace" style={{ fontSize: '0.75rem' }}>
                      {log.route}
                    </div>
                  </td>
                  <td className="text-end pe-4 align-middle">
                    <span className={`badge rounded-pill ${log.status === 'success' ? 'bg-success-subtle text-success' : 'bg-danger-subtle text-danger'}`}>
                      {log.status}
                    </span>
                  </td>
                </tr>
              ))}
              {auditLogs.length === 0 && !loading && (
                <tr>
                  <td colSpan="3" className="text-center py-5 text-secondary">
                    <i className="fa-solid fa-ghost fa-2x mb-2 opacity-25 d-block"></i>
                    No recent activity found.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
