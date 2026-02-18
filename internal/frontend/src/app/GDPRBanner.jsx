/* eslint-env browser */
/* global React, localStorage */

export default function GDPRBanner() {
  const { useState, useEffect } = React;
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    const consent = localStorage.getItem('gdpr_consent');
    if (!consent) {
      setVisible(true);
    }
  }, []);

  const accept = () => {
    localStorage.setItem('gdpr_consent', 'true');
    setVisible(false);
  };

  if (!visible) return null;

  return (
    <div className="fixed-bottom p-3 z-3">
      <div className="card border-0 shadow-lg bg-dark text-white rounded-4 overflow-hidden">
        <div className="card-body p-4">
          <div className="d-flex flex-column flex-md-row align-items-md-center gap-3">
            <div className="flex-grow-1">
              <h6 className="fw-bold mb-1">
                <i className="fa-solid fa-cookie-bite me-2 text-warning"></i>
                Cookie Consent & Privacy
              </h6>
              <p className="small mb-0 opacity-75">
                We use strictly necessary cookies for authentication and security. By using our site, you agree to our 
                <a href="#/privacy" className="text-white text-decoration-underline ms-1">Privacy Policy</a>.
              </p>
            </div>
            <div className="d-flex gap-2">
              <button className="btn btn-primary btn-sm rounded-pill px-4" onClick={accept}>
                Got it
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
