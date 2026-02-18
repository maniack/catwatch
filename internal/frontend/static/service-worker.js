// Service Worker with no caching
self.addEventListener('install', (e) => {
  // Immediately activate updated SW without waiting
  self.skipWaiting();
});

self.addEventListener('activate', (e) => {
  // Remove any previously stored caches and take control
  e.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (e) => {
  // Network-only strategy: do not use Cache Storage at all
  e.respondWith(fetch(e.request));
});
