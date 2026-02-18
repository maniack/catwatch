/* eslint-env browser */
/* global React */
import api from '../api/api.js';
import { toggleLikeCat } from '../utils/cats.js';
import useCarouselIdle from '../hooks/useCarouselIdle.js';
import useDoubleTap from '../hooks/useDoubleTap.js';

const emojiForCondition = (c) => {
  switch (c) {
    case 1: return "🙀";
    case 2: return "😿";
    case 3: return "😺";
    case 4: return "😽";
    case 5: return "😻";
    default: return "🐱";
  }
};

const labelForCondition = (c) => {
  switch (c) {
    case 1: return "Very Bad";
    case 2: return "Bad";
    case 3: return "Normal";
    case 4: return "Good";
    case 5: return "Excellent";
    default: return "Unknown";
  }
};

export default function CatView({ catId, user }) {
  const { useState, useEffect, useRef } = React;
  const [cat, setCat] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [uploading, setUploading] = useState(false);
  const [showRecordForm, setShowRecordForm] = useState(false);
  const [showLocationForm, setShowLocationForm] = useState(false);
  const [showObserveForm, setShowObserveForm] = useState(false);
  const [newRecord, setNewRecord] = useState({ type: 'feeding', note: '', planned_at: '' });
  const [newLocation, setNewLocation] = useState({ name: '', description: '', lat: '', lon: '' });
  const [observeData, setObserveData] = useState({ condition: 3, note: 'Observation via Web UI' });
  const [likes, setLikes] = useState(0);
  const [liked, setLiked] = useState(false);
  const carouselRef = useRef(null);
  const fileInputRef = useRef(null);

  useCarouselIdle(carouselRef, [cat && cat.id]);

  const handleLike = (e) => {
    if (e && e.stopPropagation) e.stopPropagation();
    toggleLikeCat(catId, user, liked, likes, setLiked, setLikes);
  };

  const onPointerUp = useDoubleTap(handleLike);

  const fetchCat = () => {
    api.get(`/api/cats/${catId}/`)
      .then(data => {
        setCat(data);
        setLikes(data.likes || 0);
        setLiked(!!data.liked);
        setObserveData(prev => ({ ...prev, condition: data.condition }));
        setLoading(false);
      })
      .catch(err => {
        setError(err.message);
        setLoading(false);
      });
  };

  useEffect(() => {
    fetchCat();
  }, [catId]);

  const handlePhotoClick = () => {
    fileInputRef.current.click();
  };

  const handlePhotoUpload = async (e) => {
    const files = e.target.files;
    if (!files || files.length === 0) return;

    setUploading(true);
    try {
      for (let i = 0; i < files.length; i++) {
        const formData = new FormData();
        formData.append('file', files[i]);
        await api.post(`/api/cats/${catId}/images`, formData);
      }
      fetchCat();
    } catch (err) {
      alert('Upload failed: ' + err.message);
    } finally {
      setUploading(false);
      e.target.value = ''; // Reset input
    }
  };


  const handleAddRecord = async (e, quickAction = null) => {
    if (e && e.preventDefault) e.preventDefault();
    
    let payload;
    if (quickAction) {
      payload = { ...quickAction, done_at: new Date().toISOString() };
    } else {
      payload = { ...newRecord };
      if (!payload.planned_at) {
        delete payload.planned_at;
        payload.done_at = new Date().toISOString();
      } else {
        payload.planned_at = new Date(payload.planned_at).toISOString();
      }
    }

    try {
      await api.post(`/api/cats/${catId}/records`, payload);
      if (!quickAction) {
        setNewRecord({ type: 'feeding', note: '', planned_at: '' });
        setShowRecordForm(false);
      }
      fetchCat();
    } catch (err) {
      alert('Failed to add record: ' + err.message);
    }
  };

  const handleAddLocation = async (e) => {
    e.preventDefault();
    try {
      const payload = {
        name: newLocation.name,
        description: newLocation.description,
        lat: parseFloat(newLocation.lat),
        lon: parseFloat(newLocation.lon)
      };
      if (isNaN(payload.lat) || isNaN(payload.lon)) {
        throw new Error('Invalid coordinates');
      }
      await api.post(`/api/cats/${catId}/locations`, payload);
      setNewLocation({ name: '', description: '', lat: '', lon: '' });
      setShowLocationForm(false);
      fetchCat();
    } catch (err) {
      alert('Failed to add location: ' + err.message);
    }
  };

  const handleGetLocation = () => {
    if (navigator.geolocation) {
      navigator.geolocation.getCurrentPosition((pos) => {
        setNewLocation(prev => ({ ...prev, lat: pos.coords.latitude.toFixed(6), lon: pos.coords.longitude.toFixed(6) }));
      }, (err) => {
        alert('Geolocation error: ' + err.message);
      });
    } else {
      alert('Geolocation not supported by this browser.');
    }
  };

  const handleMarkDone = async (rid) => {
    try {
      await api.post(`/api/cats/${catId}/records/${rid}/done`, {});
      fetchCat();
    } catch (err) {
      alert('Failed to mark as done: ' + err.message);
    }
  };

  const handleObserveSubmit = async (e) => {
    e.preventDefault();
    try {
      // 1. Update cat condition
      await api.put(`/api/cats/${catId}/`, { ...cat, condition: observeData.condition });
      
      // 2. Prepare note
      let finalNote = `Condition: ${emojiForCondition(observeData.condition)} ${labelForCondition(observeData.condition)}`;
      if (observeData.note && observeData.note !== 'Observation via Web UI') {
        finalNote += `\nNote: ${observeData.note}`;
      }

      // 3. Add record
      await api.post(`/api/cats/${catId}/records`, { 
        type: 'observation', 
        note: finalNote,
        timestamp: new Date().toISOString(),
        done_at: new Date().toISOString()
      });

      setShowObserveForm(false);
      fetchCat();
    } catch (err) {
      alert('Observation failed: ' + err.message);
    }
  };

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
    return <div className="alert alert-danger">Error: {error}</div>;
  }

  if (!cat) return null;

  return (
    <div className="row g-4">
      <div className="col-12 col-lg-8">
        <div className="card border-0 bg-body-tertiary shadow-sm rounded-4 overflow-hidden mb-4">
          <div className="row g-0">
            <div className="col-md-5 position-relative" style={{ minHeight: '300px', backgroundColor: '#333' }}>
              {cat.images && cat.images.length > 0 ? (
                <div ref={carouselRef} id="catImages" className="carousel slide h-100" data-bs-ride="carousel" data-bs-interval="false" data-bs-touch="true">
                  <div className="carousel-inner h-100">
                    {[...cat.images].sort((a,b)=>new Date(b.created_at)-new Date(a.created_at)).map((img, idx) => (
                      <div key={img.id} className={`carousel-item h-100 ${idx === 0 ? 'active' : ''}`}>
                        <img src={`/api/cats/${cat.id}/images/${img.id}`} className="d-block w-100 h-100 object-fit-cover" alt="..." style={{touchAction:'manipulation'}} onDoubleClick={handleLike} onPointerUp={onPointerUp} />
                      </div>
                    ))}
                  </div>
                  {cat.images.length > 1 && (
                    <>
                      <button className="carousel-control-prev" type="button" data-bs-target="#catImages" data-bs-slide="prev">
                        <span className="carousel-control-prev-icon" aria-hidden="true"></span>
                      </button>
                      <button className="carousel-control-next" type="button" data-bs-target="#catImages" data-bs-slide="next">
                        <span className="carousel-control-next-icon" aria-hidden="true"></span>
                      </button>
                    </>
                  )}
                </div>
              ) : (
                <div className="w-100 h-100 d-flex align-items-center justify-content-center text-secondary">
                  <i className="fa-solid fa-cat fa-6x opacity-25"></i>
                </div>
              )}
              <div className="position-absolute bottom-0 end-0 m-2" style={{zIndex: 20}}>
                <span
                  role="button"
                  tabIndex={0}
                  className={(user ? "bg-body-secondary bg-opacity-75" : "bg-secondary bg-opacity-25") + " px-2 py-1 small rounded-1 d-inline-flex align-items-center"}
                  style={{userSelect:'none', cursor: user ? 'pointer' : 'default'}}
                  onClick={handleLike}
                  title={user ? (liked ? 'Remove like' : 'Like') : 'Sign in to like'}
                >
                  <i className={"fa-solid fa-heart me-1 " + (liked || likes > 0 ? "text-danger" : "text-secondary")}></i>
                  <span>{likes}</span>
                </span>
              </div>
            </div>
            <div className="col-md-7">
              <div className="card-body p-4">
                <div className="mb-4">
                  <h2 className="card-title fw-bold mb-1">{cat.name}</h2>
                  <div>
                    {cat.need_attention && <span className="badge bg-danger me-2">⚠️ Needs attention</span>}
                    {cat.tags && cat.tags.map(t => (
                      <span key={t.id} className="badge bg-secondary me-1">#{t.name}</span>
                    ))}
                  </div>
                </div>

                <div className="mb-4 d-flex align-items-center gap-3">
                  <div>
                    <h6 className="text-secondary text-uppercase small fw-bold mb-2">Condition</h6>
                    <div className="fs-4">
                      {emojiForCondition(cat.condition)} {labelForCondition(cat.condition)}
                    </div>
                  </div>
                </div>

                <div className="mb-4">
                  <h6 className="text-secondary text-uppercase small fw-bold mb-2">Description</h6>
                  <p className="card-text">{cat.description || "No description."}</p>
                </div>

                <div className="row g-3 small">
                  <div className="col-6">
                    <div className="text-secondary text-uppercase x-small fw-bold">Gender</div>
                    <div className="fw-bold text-capitalize">{cat.gender || "Unknown"}</div>
                  </div>
                  <div className="col-6">
                    <div className="text-secondary text-uppercase x-small fw-bold">Sterilized</div>
                    <div className="fw-bold">{cat.is_sterilized ? "Yes" : "No"}</div>
                  </div>
                  <div className="col-6">
                    <div className="text-secondary text-uppercase x-small fw-bold">Color</div>
                    <div className="fw-bold">{cat.color || "Unknown"}</div>
                  </div>
                  <div className="col-6">
                    <div className="text-secondary text-uppercase x-small fw-bold">Birth Date</div>
                    <div className="fw-bold">{cat.birth_date ? new Date(cat.birth_date).toLocaleDateString() : "Unknown"}</div>
                  </div>
                  <div className="col-6">
                    <div className="text-secondary text-uppercase x-small fw-bold">Last seen</div>
                    <div>{cat.last_seen ? new Date(cat.last_seen).toLocaleString() : "Never"}</div>
                  </div>
                  <div className="col-6">
                    <div className="text-secondary text-uppercase x-small fw-bold">Registered</div>
                    <div>{new Date(cat.created_at).toLocaleDateString()}</div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>

        {user && (
          <div className="mb-4">
            <div className="d-flex gap-2 flex-wrap justify-content-center justify-content-md-start mb-3">
              <input type="file" ref={fileInputRef} onChange={handlePhotoUpload} className="d-none" accept="image/*" multiple />
              <button className="btn btn-outline-info btn-sm rounded-pill px-3 py-2" onClick={(e) => handleAddRecord(e, { type: 'observation', note: 'Marked as seen via Web UI' })}>
                <i className="fa-solid fa-eye me-1"></i> Seen
              </button>
              <button className="btn btn-outline-warning btn-sm rounded-pill px-3 py-2" onClick={(e) => handleAddRecord(e, { type: 'feeding', note: 'Quick feed via Web UI' })}>
                <i className="fa-solid fa-bowl-food me-1"></i> Feed
              </button>
              <button className="btn btn-outline-success btn-sm rounded-pill px-3 py-2" onClick={() => setShowObserveForm(!showObserveForm)}>
                <i className="fa-solid fa-magnifying-glass me-1"></i> Observe
              </button>
              <button className="btn btn-outline-secondary btn-sm rounded-pill px-3 py-2" onClick={handlePhotoClick} disabled={uploading}>
                {uploading ? <span className="spinner-border spinner-border-sm me-1"></span> : <i className="fa-solid fa-camera me-1"></i>}
                Photo
              </button>
              <a href={`#/cat/edit/${cat.id}`} className="btn btn-outline-primary btn-sm rounded-pill px-3 py-2">
                <i className="fa-solid fa-pen me-1"></i> Edit
              </a>
            </div>

            {showObserveForm && (
              <div className="p-4 bg-body-tertiary rounded-4 shadow-sm border border-success border-opacity-25">
                <h6 className="fw-bold mb-3 text-success"><i className="fa-solid fa-magnifying-glass me-2"></i>Quick Observation</h6>
                <form onSubmit={handleObserveSubmit}>
                  <div className="mb-3">
                    <label className="form-label x-small fw-bold text-uppercase">Condition</label>
                    <select className="form-select form-select-sm bg-dark border-0 text-white mb-2" value={observeData.condition} onChange={(e)=>setObserveData({...observeData, condition: parseInt(e.target.value)})}>
                      <option value="1">🙀 1 - Very Bad</option>
                      <option value="2">😿 2 - Bad</option>
                      <option value="3">😺 3 - Normal</option>
                      <option value="4">😽 4 - Good</option>
                      <option value="5">😻 5 - Excellent</option>
                    </select>
                  </div>
                  <div className="mb-3">
                    <label className="form-label x-small fw-bold text-uppercase">Observation Note</label>
                    <textarea className="form-control form-control-sm bg-dark border-0" rows="2" value={observeData.note} onChange={(e)=>setObserveData({...observeData, note: e.target.value})} placeholder="e.g. Looks good, seems healthy..."></textarea>
                  </div>
                  <div className="d-flex gap-2">
                    <button type="submit" className="btn btn-success btn-sm flex-grow-1 rounded-pill">Save Observation</button>
                    <button type="button" className="btn btn-outline-secondary btn-sm rounded-pill" onClick={()=>setShowObserveForm(false)}>Cancel</button>
                  </div>
                  <div className="mt-2 text-center">
                    <small className="text-secondary x-small">Use "Photo" and "Log Sighting" buttons separately after saving if needed.</small>
                  </div>
                </form>
              </div>
            )}
          </div>
        )}

        {/* Locations */}
        <div className="card border-0 bg-body-tertiary shadow-sm rounded-4 p-4">
          <div className="d-flex justify-content-between align-items-center mb-3">
            <h5 className="fw-bold mb-0"><i className="fa-solid fa-location-dot me-2 text-primary"></i>Sightings</h5>
            {user && (
              <button className="btn btn-outline-primary btn-sm rounded-pill" onClick={() => setShowLocationForm(!showLocationForm)}>
                <i className={`fa-solid ${showLocationForm ? 'fa-xmark' : 'fa-plus'} me-1`}></i>
                {showLocationForm ? 'Cancel' : 'Log Sighting'}
              </button>
            )}
          </div>

          {showLocationForm && (
            <form onSubmit={handleAddLocation} className="mb-4 p-3 bg-dark-subtle rounded-3 shadow-sm border border-secondary border-opacity-25">
              <div className="row g-2 mb-2">
                <div className="col-6">
                  <label className="form-label x-small fw-bold text-uppercase">Lat</label>
                  <input type="number" step="any" className="form-control form-control-sm bg-dark border-0" value={newLocation.lat} onChange={(e)=>setNewLocation({...newLocation, lat: e.target.value})} required />
                </div>
                <div className="col-6">
                  <label className="form-label x-small fw-bold text-uppercase">Lon</label>
                  <input type="number" step="any" className="form-control form-control-sm bg-dark border-0" value={newLocation.lon} onChange={(e)=>setNewLocation({...newLocation, lon: e.target.value})} required />
                </div>
              </div>
              <div className="mb-2">
                <button type="button" className="btn btn-info btn-sm w-100 rounded-pill" onClick={handleGetLocation}>
                  <i className="fa-solid fa-location-crosshairs me-1"></i> Get Current Location
                </button>
              </div>
              <div className="mb-2">
                <label className="form-label x-small fw-bold text-uppercase">Location Name (optional)</label>
                <input type="text" className="form-control form-control-sm bg-dark border-0" value={newLocation.name} onChange={(e)=>setNewLocation({...newLocation, name: e.target.value})} placeholder="e.g. Garden, Park..." />
              </div>
              <div className="mb-3">
                <label className="form-label x-small fw-bold text-uppercase">Note (optional)</label>
                <textarea className="form-control form-control-sm bg-dark border-0" rows="2" value={newLocation.description} onChange={(e)=>setNewLocation({...newLocation, description: e.target.value})} placeholder="Describe the place..."></textarea>
              </div>
              <button type="submit" className="btn btn-success btn-sm w-100 rounded-pill">Save Sighting</button>
            </form>
          )}

          {cat.locations && cat.locations.length > 0 ? (
            <div className="list-group list-group-flush bg-transparent">
              {cat.locations.sort((a,b)=>new Date(b.created_at)-new Date(a.created_at)).map(loc => (
                <div key={loc.id} className="list-group-item bg-transparent border-secondary border-opacity-25 px-0 py-3">
                  <div className="d-flex justify-content-between">
                    <h6 className="mb-1">{loc.name || `Location at ${loc.lat.toFixed(4)}, ${loc.lon.toFixed(4)}`}</h6>
                    <span className="text-secondary small">{new Date(loc.created_at).toLocaleString()}</span>
                  </div>
                  {loc.description && <p className="mb-1 small text-secondary">{loc.description}</p>}
                  <a href={`https://www.google.com/maps/search/?api=1&query=${loc.lat},${loc.lon}`} target="_blank" className="btn btn-link btn-sm p-0 text-decoration-none">
                    View on Maps
                  </a>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-secondary mb-0">No locations recorded.</p>
          )}
        </div>
      </div>

      <div className="col-12 col-lg-4">
        {/* Records / Journal */}
        <div className="card border-0 bg-body-tertiary shadow-sm rounded-4 p-4 h-100">
          <div className="d-flex justify-content-between align-items-center mb-3">
            <h5 className="fw-bold mb-0"><i className="fa-solid fa-clipboard-list me-2 text-primary"></i>Journal</h5>
            {user && (
              <button className="btn btn-primary btn-sm rounded-pill" onClick={() => setShowRecordForm(!showRecordForm)}>
                <i className={`fa-solid ${showRecordForm ? 'fa-xmark' : 'fa-plus'} me-1`}></i>
                {showRecordForm ? 'Cancel' : 'Add Entry'}
              </button>
            )}
          </div>

          {showRecordForm && (
            <form onSubmit={handleAddRecord} className="mb-4 p-3 bg-dark-subtle rounded-3 shadow-sm border border-secondary border-opacity-25">
              <div className="mb-2">
                <label className="form-label x-small fw-bold text-uppercase">Type</label>
                <select className="form-select form-select-sm bg-dark border-0" value={newRecord.type} onChange={(e)=>setNewRecord({...newRecord, type: e.target.value})}>
                  <option value="feeding">Feeding</option>
                  <option value="observation">Observation</option>
                  <option value="medical">Medical</option>
                  <option value="vaccination">Vaccination</option>
                  <option value="vet_visit">Vet Visit</option>
                  <option value="sterilization">Sterilization</option>
                  <option value="medication">Medication</option>
                </select>
              </div>
              <div className="mb-2">
                <label className="form-label x-small fw-bold text-uppercase">Note</label>
                <textarea className="form-control form-control-sm bg-dark border-0" rows="2" value={newRecord.note} onChange={(e)=>setNewRecord({...newRecord, note: e.target.value})} placeholder="Describe what happened..."></textarea>
              </div>
              <div className="mb-3">
                <label className="form-label x-small fw-bold text-uppercase">Planned Time (optional)</label>
                <input type="datetime-local" className="form-control form-control-sm bg-dark border-0" value={newRecord.planned_at} onChange={(e)=>setNewRecord({...newRecord, planned_at: e.target.value})} />
              </div>
              <button type="submit" className="btn btn-success btn-sm w-100 rounded-pill">Save Entry</button>
            </form>
          )}

          {cat.records && cat.records.length > 0 ? (
            <div className="timeline">
              {[...cat.records].sort((a,b)=>new Date(b.timestamp || b.created_at)-new Date(a.timestamp || a.created_at)).map(rec => (
                <div key={rec.id} className="mb-4 position-relative ps-4 border-start border-secondary border-opacity-50">
                  <div className="position-absolute start-0 top-0 translate-middle-x bg-body-tertiary border border-secondary p-1 rounded-circle" style={{ marginLeft: '-1px' }}>
                    <div className={`rounded-circle ${rec.done_at ? 'bg-success' : 'bg-warning'}`} style={{ width: '8px', height: '8px' }}></div>
                  </div>
                  <div className="d-flex justify-content-between align-items-start mb-1">
                    <div>
                      <span className="fw-bold text-capitalize">{rec.type.replace('_',' ')}</span>
                      {!rec.done_at && rec.planned_at && user && (
                        <button className="btn btn-link btn-sm text-success p-0 ms-2 text-decoration-none x-small" onClick={() => handleMarkDone(rec.id)}>
                          <i className="fa-solid fa-check me-1"></i>Mark Done
                        </button>
                      )}
                    </div>
                    <span className="x-small text-secondary">{new Date(rec.done_at || rec.planned_at || rec.timestamp).toLocaleString()}</span>
                  </div>
                  {rec.note && <p className="small text-secondary mb-0">{rec.note}</p>}
                  {rec.done_at && <span className="badge bg-success-subtle text-success border border-success border-opacity-25 x-small">Completed</span>}
                  {rec.planned_at && !rec.done_at && <span className="badge bg-warning-subtle text-warning border border-warning border-opacity-25 x-small">Planned</span>}
                </div>
              ))}
            </div>
          ) : (
            <p className="text-secondary mb-0">No journal entries yet.</p>
          )}
        </div>
      </div>
    </div>
  );
}
