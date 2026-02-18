/* eslint-env browser */
/* global React */
import api from '../api/api.js';
import CatCard from '../components/CatCard.jsx';

export default function MainView({ user }) {
  const { useState, useEffect } = React;
  const [cats, setCats] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    api.get('/api/cats/')
      .then(data => {
        setCats(data);
        setLoading(false);
      })
      .catch(err => {
        setError(err.message);
        setLoading(false);
      });
  }, []);

  if (loading) {
    return (
      <div className="text-center py-5">
        <div className="spinner-border text-primary" role="status">
          <span className="visually-hidden">Loading...</span>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="alert alert-danger" role="alert">
        Error loading cats: {error}
      </div>
    );
  }

  return (
    <div>
      <div className="d-flex justify-content-end align-items-center mb-4">
        {user && (
          <a href="#/cat/new" className="btn btn-primary rounded-pill px-4">
            <i className="fa-solid fa-plus me-2"></i>
            Add New Cat
          </a>
        )}
      </div>

      {cats.length === 0 ? (
        <div className="text-center py-5 bg-body-tertiary rounded-4">
          <i className="fa-solid fa-cat fa-4x mb-3 opacity-25"></i>
          <p className="lead text-secondary">No cats found in the registry.</p>
        </div>
      ) : (
        <div className="row row-cols-1 row-cols-sm-2 row-cols-md-3 row-cols-lg-4 g-4">
          {cats.map(cat => (
            <div key={cat.id} className="col">
              <CatCard cat={cat} user={user} />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
