/* eslint-env browser */
/* global window, localStorage */
const api = {
  async request(path, opts = {}) {
    const headers = opts.headers || {};
    // Do not attach Authorization header; rely on HttpOnly cookies
    const isForm = (typeof FormData !== 'undefined') && (opts.body instanceof FormData);
    if (!isForm && !headers['Content-Type']) headers['Content-Type'] = 'application/json';
    const res = await fetch(path, { ...opts, headers, credentials: 'include' });
    if (res.status === 401) {
      throw new Error('unauthorized');
    }
    if (res.status === 204) return null;
    const json = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(json.error || ('HTTP ' + res.status));
    return json;
  },
  get(path, opts) { return this.request(path, opts); },
  post(path, body) {
    const isForm = (typeof FormData !== 'undefined') && (body instanceof FormData);
    return this.request(path, isForm ? { method: 'POST', body } : { method: 'POST', body: JSON.stringify(body) });
  },
  put(path, body) {
    const isForm = (typeof FormData !== 'undefined') && (body instanceof FormData);
    return this.request(path, isForm ? { method: 'PUT', body } : { method: 'PUT', body: JSON.stringify(body) });
  },
  patch(path, body) {
    const isForm = (typeof FormData !== 'undefined') && (body instanceof FormData);
    return this.request(path, isForm ? { method: 'PATCH', body } : { method: 'PATCH', body: JSON.stringify(body) });
  },
  del(path) { return this.request(path, { method: 'DELETE' }); }
};
export default api;
