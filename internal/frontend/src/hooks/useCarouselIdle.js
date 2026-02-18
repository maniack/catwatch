/* eslint-env browser */
/* global React, bootstrap */

export default function useCarouselIdle(ref, deps = []) {
  const { useEffect } = React;
  useEffect(() => {
    const el = ref && ('current' in ref) ? ref.current : null;
    if (!el || typeof bootstrap === 'undefined' || !bootstrap.Carousel) return;
    const instance = bootstrap.Carousel.getInstance(el) || new bootstrap.Carousel(el, {
      interval: false,
      ride: false,
      touch: true,
      keyboard: true,
      pause: false,
      wrap: true,
    });
    const stop = () => { try { instance.pause && instance.pause(); } catch(e) {} };
    el.addEventListener('slide.bs.carousel', stop);
    el.addEventListener('slid.bs.carousel', stop);
    el.addEventListener('mouseenter', stop);
    el.addEventListener('touchstart', stop, { passive: true });
    return () => {
      try { instance.dispose && instance.dispose(); } catch(e) {}
      el.removeEventListener('slide.bs.carousel', stop);
      el.removeEventListener('slid.bs.carousel', stop);
      el.removeEventListener('mouseenter', stop);
      el.removeEventListener('touchstart', stop);
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
}
