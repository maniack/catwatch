/* eslint-env browser */
/* global React */
import { toggleLikeCat } from '../utils/cats.js';
import useDoubleTap from '../hooks/useDoubleTap.js';

export default function CatCard({ cat, user, onUnliked }) {
  const { useState, useEffect } = React;
  const { id, name, description, condition, need_attention, images, last_seen, tags, color } = cat;
  
  const [likes, setLikes] = useState(cat.likes || 0);
  const [liked, setLiked] = useState(!!cat.liked);

  useEffect(() => {
    setLikes(cat.likes || 0);
    setLiked(!!cat.liked);
  }, [cat.id, cat.likes, cat.liked]);

  const handleLike = (e) => {
    if (e && e.preventDefault) e.preventDefault();
    if (e && e.stopPropagation) e.stopPropagation();
    toggleLikeCat(id, user, liked, likes, setLiked, setLikes, onUnliked);
  };

  const onPointerUp = useDoubleTap(handleLike);
  
  const freshImage = images && images.length > 0 
    ? [...images].sort((a, b) => new Date(b.created_at) - new Date(a.created_at))[0]
    : null;

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

  const statusBadge = need_attention 
    ? <span className="badge bg-danger me-1">⚠️ Needs attention</span>
    : null;

  return (
    <div className="card h-100 shadow-sm border-0 bg-body-tertiary overflow-hidden">
      <div className="position-relative" style={{ height: '200px', backgroundColor: '#333' }}>
        {freshImage ? (
          <img 
            src={`/api/cats/${id}/images/${freshImage.id}`} 
            className="card-img-top w-100 h-100 object-fit-cover" 
            alt={name} 
            onPointerUp={onPointerUp}
            onDoubleClick={handleLike}
            style={{touchAction:'manipulation'}}
          />
        ) : (
          <div className="w-100 h-100 d-flex align-items-center justify-content-center text-secondary">
            <i className="fa-solid fa-cat fa-4x opacity-25"></i>
          </div>
        )}
        <div className="position-absolute top-0 end-0 p-2 d-flex flex-column gap-2 align-items-end" style={{zIndex: 10}}>
          <span className="badge bg-dark bg-opacity-75 fs-6">
            {emojiForCondition(condition)} {labelForCondition(condition)}
          </span>
        </div>
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
      <div className="card-body d-flex flex-column">
        <h5 className="card-title fw-bold mb-1">
          <a href={`#/cat/view/${id}`} className="text-decoration-none text-reset">{name}</a>
        </h5>
        <div className="mb-2">
          {statusBadge}
          {color && <span className="badge bg-secondary-subtle text-secondary border border-secondary border-opacity-25 me-1">{color}</span>}
          {tags && tags.map(t => (
            <span key={t.id} className="badge bg-secondary me-1">#{t.name}</span>
          ))}
        </div>
        <p className="card-text small text-secondary flex-grow-1 text-truncate-2">
          {description || "No description provided."}
        </p>
        <div className="mt-2 pt-2 border-top d-flex justify-content-between align-items-center">
          <span className="small text-body-tertiary">
            <i className="fa-solid fa-clock me-1"></i>
            {last_seen ? new Date(last_seen).toLocaleDateString() : "Never seen"}
          </span>
          <a href={`#/cat/view/${id}`} className="btn btn-primary btn-sm rounded-pill px-3">
            Details
          </a>
        </div>
      </div>
    </div>
  );
}
