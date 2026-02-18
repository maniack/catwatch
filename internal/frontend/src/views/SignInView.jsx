/* eslint-env browser */
/* global React */

export default function SignInView({ config }) {
  const { googleEnabled, oidcEnabled, devLogin, authorizationEndpoint } = config;

  return (
    <div className="row justify-content-center py-5">
      <div className="col-12 col-sm-8 col-md-6 col-lg-4 text-center">
        <h3 className="mb-4">Sign in to CatWatch</h3>
        <p className="text-secondary mb-4">You will be redirected to the secure login page.</p>
        
        <div className="d-grid gap-3">
          {(googleEnabled || oidcEnabled || devLogin) ? (
            <a href={authorizationEndpoint || '/auth/login'} className="btn btn-primary btn-lg py-3">
              <i className="fa-solid fa-right-to-bracket me-2"></i>
              Proceed to Login
            </a>
          ) : (
            <div className="alert alert-warning">
              No authorization methods are configured.
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
