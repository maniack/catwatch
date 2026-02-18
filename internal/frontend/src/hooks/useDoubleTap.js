/* eslint-env browser */
/* global React */

export default function useDoubleTap(callback, threshold = 300, maxDistancePx = 30) {
  const lastTapRef = React.useRef({ t: 0, x: 0, y: 0 });
  return React.useCallback((e) => {
    try {
      const now = Date.now();
      const x = (e.clientX || (e.changedTouches && e.changedTouches[0] && e.changedTouches[0].clientX) || 0);
      const y = (e.clientY || (e.changedTouches && e.changedTouches[0] && e.changedTouches[0].clientY) || 0);
      const last = lastTapRef.current;
      const dt = now - (last.t || 0);
      const dx = x - (last.x || 0);
      const dy = y - (last.y || 0);
      const dist2 = dx*dx + dy*dy;
      const isTouch = (e.pointerType === 'touch') || (e.type && e.type.indexOf('touch') === 0);
      if (isTouch && dt > 0 && dt < threshold && dist2 < (maxDistancePx*maxDistancePx)) {
        if (e.preventDefault) e.preventDefault();
        if (e.stopPropagation) e.stopPropagation();
        try { callback && callback(e); } catch(_) {}
        lastTapRef.current = { t: 0, x: 0, y: 0 };
      } else {
        lastTapRef.current = { t: now, x, y };
      }
    } catch(_) { /* noop */ }
  }, [callback, threshold, maxDistancePx]);
}
