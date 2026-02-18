/* eslint-env browser */
/* global React */
export default function Avatar({ user, size = 32, className = "", title = "" }) {
  const { name, avatar_url } = user || {};
  const style = {
    width: size + 'px',
    height: size + 'px',
    borderRadius: '50%',
    objectFit: 'cover'
  };

  if (avatar_url) {
    return <img src={avatar_url} style={style} className={className} alt={name} title={title || name} />;
  }

  const initial = name ? name.charAt(0).toUpperCase() : '?';
  const bgStyle = {
    ...style,
    backgroundColor: '#6c757d',
    color: 'white',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: (size / 2) + 'px',
    fontWeight: 'bold'
  };

  return <div style={bgStyle} className={className} title={title || name}>{initial}</div>;
}
