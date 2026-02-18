/* eslint-env browser */
/* global React */

export default function SignInView({ config }) {
  const { googleEnabled, oidcEnabled, devLogin } = config;

  return (
    <div className="row justify-content-center py-5">
      <div className="col-12 col-sm-8 col-md-6 col-lg-4 text-center">
        <h3 className="mb-4">Sign in to CatWatch</h3>
        <p className="text-secondary mb-4">Choose an authorization method to manage the cat registry.</p>
        
        <div className="d-grid gap-3">
          {googleEnabled && (
            <a href="/auth/google/login" className="btn btn-outline-primary btn-lg py-3">
              <i className="fa-brands fa-google me-2"></i>
              Sign in with Google
            </a>
          )}
          
          {oidcEnabled && (
            <a href="/auth/oidc/login" className="btn btn-outline-info btn-lg py-3">
              <i className="fa-solid fa-key me-2"></i>
              Sign in with OIDC
            </a>
          )}

          {devLogin && (
            <a href="/auth/dev/login" className="btn btn-outline-warning btn-lg py-3">
              <i className="fa-solid fa-vial me-2"></i>
              Sign in with Dev Login
            </a>
          )}

          {!googleEnabled && !oidcEnabled && !devLogin && (
            <div className="alert alert-warning">
              No authorization methods are configured.
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
