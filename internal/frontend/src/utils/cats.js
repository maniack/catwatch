import api from '../api/api.js';

export async function toggleLikeCat(catId, user, liked, likes, setLiked, setLikes, onUnliked) {
  if (!user) {
    window.location.hash = '#/signin';
    return;
  }
  const prevLiked = !!liked;
  const prevLikes = Number(likes) || 0;
  const nextLiked = !prevLiked;
  
  // Optimistic UI
  setLiked(nextLiked);
  setLikes(prevLikes + (nextLiked ? 1 : -1));

  try {
    const res = await api.post(`/api/cats/${catId}/like`, {});
    if (res) {
      if (typeof res.liked === 'boolean') setLiked(res.liked);
      if (typeof res.likes === 'number') setLikes(res.likes);
      if (prevLiked && !res.liked && onUnliked) {
        onUnliked(catId);
      }
    }
  } catch (err) {
    // Rollback
    setLiked(prevLiked);
    setLikes(prevLikes);
    alert('Error: ' + err.message);
  }
}
