// Service worker. Phase 7: only what installing and push need. It caches
// nothing and passes every request to the network; offline reading and
// push handling come in phases 8 and 9.
self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) => event.waitUntil(self.clients.claim()));
// A fetch handler (even a pass-through one) is what some browsers need to
// offer installing the app.
self.addEventListener("fetch", () => {});
