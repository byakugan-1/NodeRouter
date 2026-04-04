// NodeRouter Service Worker - PWA Support
self.addEventListener('install', function(e) {
  self.skipWaiting();
});

self.addEventListener('activate', function(e) {
  return self.clients.claim();
});

self.addEventListener('fetch', function(e) {
  // Network-first strategy for API, cache-first for static assets
  if (e.request.url.includes('/sse/') || e.request.url.includes('/api/')) {
    e.respondWith(fetch(e.request).catch(function() {
      return new Response('Offline', { status: 503 });
    }));
  } else {
    e.respondWith(
      caches.match(e.request).then(function(r) {
        return r || fetch(e.request).then(function(response) {
          return caches.open('noderouter-v1').then(function(cache) {
            cache.put(e.request, response.clone());
            return response;
          });
        });
      })
    );
  }
});
