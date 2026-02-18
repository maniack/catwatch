/* eslint-env browser */
/* global React, window */
import api from '../api/api.js';

export default function CatEditView({ catId, user }) {
  const { useState, useEffect, useRef } = React;
  const [cat, setCat] = useState({
    name: '',
    description: '',
    color: '',
    birth_date: '',
    condition: 3,
    gender: 'unknown',
    is_sterilized: false,
    need_attention: false,
    tags: [],
    images: []
  });
  const [tagInput, setTagInput] = useState('');
  const [loading, setLoading] = useState(!!catId);
  const [saving, setSave] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState(null);
  const fileInputRef = useRef(null);

  const fetchCat = () => {
    if (catId) {
      api.get(`/api/cats/${catId}/`)
        .then(data => {
          if (data.birth_date) {
            data.birth_date = data.birth_date.substring(0, 10);
          }
          setCat({
            ...data,
            tags: data.tags || [],
            images: data.images || [],
            color: data.color || '',
            description: data.description || '',
            gender: data.gender || 'unknown',
            birth_date: data.birth_date || ''
          });
          setLoading(false);
        })
        .catch(err => {
          setError(err.message);
          setLoading(false);
        });
    }
  };

  useEffect(() => {
    fetchCat();
  }, [catId]);

  if (loading) return <div className="text-center py-5"><div className="spinner-border"></div></div>;

  const handleChange = (e) => {
    const { name, value, type, checked } = e.target;
    setCat(prev => ({
      ...prev,
      [name]: type === 'checkbox' ? checked : (name === 'condition' ? parseInt(value) : value)
    }));
  };

  const handleAddTag = (e) => {
    if (e.key === 'Enter' && tagInput.trim()) {
      e.preventDefault();
      const tags = cat.tags || [];
      if (!tags.find(t => t.name.toLowerCase() === tagInput.trim().toLowerCase())) {
        setCat(prev => ({
          ...prev,
          tags: [...(prev.tags || []), { name: tagInput.trim() }]
        }));
      }
      setTagInput('');
    }
  };

  const removeTag = (name) => {
    setCat(prev => ({
      ...prev,
      tags: (prev.tags || []).filter(t => t.name !== name)
    }));
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    setSave(true);
    setError(null);
    try {
      const payload = { ...cat };
      if (!payload.birth_date) {
        delete payload.birth_date;
      } else {
        payload.birth_date = new Date(payload.birth_date).toISOString();
      }

      if (catId) {
        await api.put(`/api/cats/${catId}/`, payload);
      } else {
        const newCat = await api.post('/api/cats/', payload);
        window.location.hash = `#/cat/view/${newCat.id}`;
        return;
      }
      window.location.hash = `#/cat/view/${catId}`;
    } catch (err) {
      setError(err.message);
      setSave(false);
    }
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
      e.target.value = '';
    }
  };

  const handleDeletePhoto = async (imgId) => {
    if (!confirm('Are you sure you want to delete this photo?')) return;
    try {
      await api.del(`/api/cats/${catId}/images/${imgId}`);
      fetchCat();
    } catch (err) {
      alert('Delete failed: ' + err.message);
    }
  };

  const handleDeleteCat = async () => {
    if (!confirm('Are you sure you want to delete this cat? This action is reversible but the cat will disappear from the list.')) return;
    setSave(true);
    try {
      await api.del(`/api/cats/${catId}/`);
      window.location.hash = '#/';
    } catch (err) {
      setError(err.message);
      setSave(false);
    }
  };

  return (
    <div className="row justify-content-center">
      <div className="col-12 col-md-10 col-lg-8">
        <div className="card border-0 bg-body-tertiary shadow-sm rounded-4 p-4 p-md-5">
          <h2 className="fw-bold mb-4">{catId ? 'Edit Cat' : 'Add New Cat'}</h2>
          
          {error && <div className="alert alert-danger">{error}</div>}

          <form onSubmit={handleSubmit}>
            <div className="row g-3">
              <div className="col-12">
                <label className="form-label text-secondary text-uppercase small fw-bold">Name</label>
                <input type="text" name="name" className="form-control form-control-lg bg-dark border-0 rounded-3" value={cat.name} onChange={handleChange} required placeholder="Cat's name" />
              </div>

              <div className="col-12">
                <label className="form-label text-secondary text-uppercase small fw-bold">Description</label>
                <textarea name="description" className="form-control bg-dark border-0 rounded-3" rows="3" value={cat.description} onChange={handleChange} placeholder="Physical features, behavior, etc."></textarea>
              </div>

              <div className="col-md-6">
                <label className="form-label text-secondary text-uppercase small fw-bold">Color</label>
                <input type="text" name="color" className="form-control bg-dark border-0 rounded-3" value={cat.color || ''} onChange={handleChange} placeholder="e.g. Black, Ginger, Tortie" />
              </div>

              <div className="col-md-6">
                <label className="form-label text-secondary text-uppercase small fw-bold">Birth Date</label>
                <input type="date" name="birth_date" className="form-control bg-dark border-0 rounded-3" value={cat.birth_date || ''} onChange={handleChange} />
              </div>

              <div className="col-md-6">
                <label className="form-label text-secondary text-uppercase small fw-bold">Condition</label>
                <select name="condition" className="form-select bg-dark border-0 rounded-3 text-white" value={cat.condition} onChange={handleChange}>
                  <option value="1">🙀 1 - Very Bad</option>
                  <option value="2">😿 2 - Bad</option>
                  <option value="3">😺 3 - Normal</option>
                  <option value="4">😽 4 - Good</option>
                  <option value="5">😻 5 - Excellent</option>
                </select>
              </div>

              <div className="col-md-6">
                <label className="form-label text-secondary text-uppercase small fw-bold">Gender</label>
                <select name="gender" className="form-select bg-dark border-0 rounded-3" value={cat.gender} onChange={handleChange}>
                  <option value="unknown">Unknown</option>
                  <option value="male">Male</option>
                  <option value="female">Female</option>
                </select>
              </div>

              <div className="col-md-6">
                <div className="form-check form-switch mt-2">
                  <input className="form-check-input" type="checkbox" name="is_sterilized" id="chkSterilized" checked={cat.is_sterilized} onChange={handleChange} />
                  <label className="form-check-label" htmlFor="chkSterilized">Sterilized</label>
                </div>
              </div>

              <div className="col-md-6">
                <div className="form-check form-switch mt-2">
                  <input className="form-check-input" type="checkbox" name="need_attention" id="chkAttention" checked={cat.need_attention} onChange={handleChange} />
                  <label className="form-check-label" htmlFor="chkAttention">Needs attention ⚠️</label>
                </div>
              </div>

              <div className="col-12">
                <label className="form-label text-secondary text-uppercase small fw-bold">Tags</label>
                <input type="text" className="form-control bg-dark border-0 rounded-3 mb-2" value={tagInput} onChange={(e)=>setTagInput(e.target.value)} onKeyDown={handleAddTag} placeholder="Type tag and press Enter" />
                <div>
                  {(cat.tags || []).map(t => (
                    <span key={t.name} className="badge bg-secondary me-1 py-2 px-3 rounded-pill">
                      #{t.name}
                      <button type="button" className="btn-close btn-close-white ms-2" style={{fontSize: '0.5rem'}} onClick={()=>removeTag(t.name)}></button>
                    </span>
                  ))}
                </div>
              </div>

              {catId && (
                <div className="col-12 mt-4">
                  <div className="d-flex justify-content-between align-items-center mb-2">
                    <label className="form-label text-secondary text-uppercase small fw-bold mb-0">Photos</label>
                    <button type="button" className="btn btn-outline-primary btn-sm rounded-pill px-3" onClick={()=>fileInputRef.current.click()} disabled={uploading}>
                      <i className="fa-solid fa-plus me-1"></i> {uploading ? 'Uploading...' : 'Add Photo'}
                    </button>
                  </div>
                  <input type="file" ref={fileInputRef} className="d-none" accept="image/*" multiple onChange={handlePhotoUpload} />
                  <div className="row g-2">
                    {cat.images && cat.images.length > 0 ? cat.images.map(img => (
                      <div key={img.id} className="col-4 col-md-3 col-lg-2">
                        <div className="position-relative ratio ratio-1x1 rounded-3 overflow-hidden bg-dark">
                          <img src={`/api/cats/${catId}/images/${img.id}`} className="object-fit-cover w-100 h-100" alt="" />
                          <div className="position-absolute top-0 end-0 m-1" style={{ zIndex: 10 }}>
                            <span
                              role="button"
                              tabIndex={0}
                              className="bg-body-secondary bg-opacity-75 px-1 rounded-1 d-inline-flex align-items-center text-danger shadow-sm"
                              style={{userSelect:'none', cursor: 'pointer', fontSize: '0.8rem'}}
                              onClick={() => handleDeletePhoto(img.id)}
                              title="Delete photo"
                            >
                              <i className="fa-solid fa-trash-can"></i>
                            </span>
                          </div>
                        </div>
                      </div>
                    )) : (
                      <div className="col-12"><p className="text-secondary small">No photos uploaded yet.</p></div>
                    )}
                  </div>
                </div>
              )}

              <div className="col-12 pt-4 d-flex gap-2 flex-wrap">
                <button type="submit" className="btn btn-primary btn-lg rounded-pill px-5" disabled={saving}>
                  {saving ? <span className="spinner-border spinner-border-sm me-2"></span> : null}
                  {catId ? 'Save Changes' : 'Create Cat'}
                </button>
                <button type="button" className="btn btn-outline-secondary btn-lg rounded-pill px-4" onClick={()=>window.history.back()}>Cancel</button>
                {catId && (
                  <button type="button" className="btn btn-outline-danger btn-lg rounded-pill px-4 ms-md-auto" onClick={handleDeleteCat} disabled={saving}>
                    Delete Cat
                  </button>
                )}
              </div>
            </div>
          </form>
        </div>
      </div>
    </div>
  );
}
