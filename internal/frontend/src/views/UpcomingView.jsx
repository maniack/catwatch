/* eslint-env browser */
/* global React */
import api from '../api/api.js';

export default function UpcomingView({ user }) {
  const { useState, useEffect } = React;
  const [records, setRecords] = useState([]);
  const [cats, setCats] = useState({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    const fetchUpcoming = async () => {
      try {
        const now = new Date();
        const end = new Date();
        end.setDate(now.getDate() + 7);

        const [recs, allCats] = await Promise.all([
          api.get(`/api/records/planned?start=${now.toISOString()}&end=${end.toISOString()}`),
          api.get('/api/cats/')
        ]);

        const catMap = {};
        allCats.forEach(c => { catMap[c.id] = c; });

        setRecords(recs);
        setCats(catMap);
        setLoading(false);
      } catch (err) {
        setError(err.message);
        setLoading(false);
      }
    };

    fetchUpcoming();
  }, []);

  const handleMarkDone = async (catId, rid) => {
    try {
      await api.post(`/api/cats/${catId}/records/${rid}/done`, {});
      // Refresh
      const now = new Date();
      const end = new Date();
      end.setDate(now.getDate() + 7);
      const recs = await api.get(`/api/records/planned?start=${now.toISOString()}&end=${end.toISOString()}`);
      setRecords(recs);
    } catch (err) {
      alert('Failed to mark as done: ' + err.message);
    }
  };

  if (loading) return <div className="text-center py-5"><div className="spinner-border text-primary"></div></div>;
  if (error) return <div className="alert alert-danger">Error: {error}</div>;

  return (
    <div className="container">
      <div className="d-flex justify-content-between align-items-center mb-4">
        <h2 className="mb-0 fw-bold"><i className="fa-solid fa-calendar-week me-2 text-primary"></i>Upcoming Events</h2>
        <span className="badge bg-secondary rounded-pill">{records.length} events</span>
      </div>

      {records.length === 0 ? (
        <div className="text-center py-5 bg-body-tertiary rounded-4 shadow-sm">
          <i className="fa-solid fa-calendar-check fa-4x mb-3 opacity-25"></i>
          <p className="lead text-secondary">No upcoming events for the next 7 days.</p>
        </div>
      ) : (
        <div className="card border-0 bg-body-tertiary shadow-sm rounded-4 overflow-hidden">
          <div className="list-group list-group-flush bg-transparent">
            {records.map(rec => {
              const cat = cats[rec.cat_id] || { name: 'Unknown Cat' };
              const time = new Date(rec.planned_at || rec.timestamp);
              return (
                <div key={rec.id} className="list-group-item bg-transparent border-secondary border-opacity-25 p-4">
                  <div className="row align-items-center g-3">
                    <div className="col-auto">
                      <div className="bg-primary bg-opacity-10 text-primary rounded-3 p-3 text-center" style={{ width: '70px' }}>
                        <div className="fw-bold fs-4">{time.getDate()}</div>
                        <div className="x-small text-uppercase">{time.toLocaleString('en', { month: 'short' })}</div>
                      </div>
                    </div>
                    <div className="col">
                      <div className="d-flex justify-content-between align-items-start mb-1">
                        <h5 className="mb-0 fw-bold">
                          <a href={`#/cat/view/${rec.cat_id}`} className="text-decoration-none text-reset">
                            {cat.name}
                          </a>
                          <span className="ms-2 badge bg-info-subtle text-info border border-info border-opacity-25 x-small text-capitalize">
                            {rec.type.replace('_',' ')}
                          </span>
                        </h5>
                        <span className="text-secondary small fw-bold">
                          <i className="fa-regular fa-clock me-1"></i>
                          {time.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                        </span>
                      </div>
                      {rec.note && <p className="mb-0 small text-secondary fst-italic">"{rec.note}"</p>}
                      {rec.recurrence && (
                        <div className="mt-1 x-small text-info opacity-75">
                          <i className="fa-solid fa-arrows-rotate me-1"></i>
                          Every {rec.interval} {rec.recurrence}
                        </div>
                      )}
                    </div>
                    {user && (
                      <div className="col-12 col-md-auto text-md-end">
                        <button className="btn btn-outline-success btn-sm rounded-pill px-3" onClick={() => handleMarkDone(rec.cat_id, rec.id)}>
                          <i className="fa-solid fa-check me-1"></i> Mark Done
                        </button>
                      </div>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}
