/* eslint-env browser */
/* global React */

export default function PrivacyView() {
  return (
    <div className="container py-5" style={{ maxWidth: '800px' }}>
      <div className="card border-0 bg-body-tertiary shadow-sm rounded-4 p-5">
        <h1 className="fw-bold mb-4">Privacy Policy</h1>
        <p className="lead text-secondary mb-5">
          We care about your privacy and the protection of your personal data.
        </p>

        <section className="mb-5">
          <h4 className="fw-bold mb-3">1. Data Controller</h4>
          <p>
            The CatWatch community project (Housing Cooperative Neighbors) operates this service. 
            Data is stored and processed within the European Union (Cyprus).
          </p>
        </section>

        <section className="mb-5">
          <h4 className="fw-bold mb-3">2. Data We Collect</h4>
          <ul className="list-group list-group-flush bg-transparent">
            <li className="list-group-item bg-transparent border-0 px-0">
              <strong>Account Information:</strong> Name, email address, and profile picture (via OAuth providers).
            </li>
            <li className="list-group-item bg-transparent border-0 px-0">
              <strong>Volunteer Activity:</strong> Records of cat feeding/checks, liked cats, and associations with Telegram chats.
            </li>
            <li className="list-group-item bg-transparent border-0 px-0">
              <strong>Audit Logs:</strong> Records of your mutating actions (create, update, delete) to ensure system integrity. 
              We do not store your IP address or browser fingerprints in our database permanently.
            </li>
          </ul>
        </section>

        <section className="mb-5">
          <h4 className="fw-bold mb-3">3. Purpose and Legal Basis</h4>
          <p>
            We process your data based on <strong>Contractual Necessity</strong> (to provide the service you signed up for) 
            and <strong>Legitimate Interest</strong> (to coordinate cat care and prevent abuse).
          </p>
        </section>

        <section className="mb-5">
          <h4 className="fw-bold mb-3">4. Cookies</h4>
          <p>
            We use strictly necessary cookies for authentication (`access_token`, `refresh_token`). 
            These are required for the site to function securely.
          </p>
        </section>

        <section className="mb-5">
          <h4 className="fw-bold mb-3">5. Third-Party Services</h4>
          <p>
            - <strong>OAuth Providers (Google/OIDC):</strong> For secure authentication.<br/>
            - <strong>Telegram:</strong> If you choose to link your bot notifications.<br/>
            - <strong>CloudFlare:</strong> For security and performance optimization (operates as a processor).
          </p>
        </section>

        <section className="mb-5">
          <h4 className="fw-bold mb-3">6. Your Rights (GDPR)</h4>
          <p>
            Under GDPR, you have the right to:
          </p>
          <ul>
            <li>Access and export your data.</li>
            <li>Rectify inaccurate data (via Profile page).</li>
            <li>Erase your data (via Profile page - "Delete Account").</li>
            <li>Object to processing or restrict it.</li>
          </ul>
          <p>
            To exercise your rights or if you have questions, please contact the community volunteer leads.
          </p>
        </section>

        <div className="mt-5 pt-4 border-top text-secondary small">
          Last updated: February 18, 2026
        </div>
      </div>
    </div>
  );
}
