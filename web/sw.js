// Service worker: keeps the app shell available offline. The server puts
// VERSION and APP_FILES in front of this file (sw.go).
//
// - App files: cached at install in a cache named after VERSION; the network
//   wins while it answers, so a new build shows up on the next load.
// - The API goes straight to the network: the page keeps its own snapshot of
//   the user's data (offline.js), so it always knows when it is offline.
// - Logging in or out drops that snapshot: it belongs to one user.

const APP_CACHE = "leitner-app-" + VERSION;
const DATA_CACHE = "leitner-data"; // filled by the page (offline.js)
// Funnel answers like this while the server is down or restarting.
const GATEWAY_ERRORS = new Set([502, 503, 504]);
// These requests change who is logged in.
const SESSION_PATHS = new Set(["/login", "/logout", "/logout-all"]);
// A page load waits this long for the network before the cached app opens.
const NAVIGATE_TIMEOUT = 5000; // ms

async function precache() {
  const cache = await caches.open(APP_CACHE);
  await Promise.all(
    APP_FILES.map(async (url) => {
      const res = await fetch(url, { cache: "reload" });
      if (res.ok && !res.redirected) {
        await cache.put(url, res);
      } else if (url !== "/") {
        // Without a session "/" redirects to the login page; the next visit stores it.
        throw new Error(`${url}: ${res.status}`);
      }
    }),
  );
}

async function dropOldCaches() {
  for (const name of await caches.keys()) {
    if (name.startsWith("leitner-app-") && name !== APP_CACHE) {
      await caches.delete(name);
    }
  }
}

self.addEventListener("install", (e) => {
  e.waitUntil(precache().then(() => self.skipWaiting()));
});

self.addEventListener("activate", (e) => {
  e.waitUntil(dropOldCaches().then(() => self.clients.claim()));
});

// fetchWithin is fetch that gives up after ms (0: no limit).
function fetchWithin(req, ms) {
  if (!ms) {
    return fetch(req);
  }
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("timeout")), ms);
    fetch(req).then(resolve, reject).finally(() => clearTimeout(timer));
  });
}

// networkFirst answers from the network and falls back to the copy cached
// under key when the server can't be reached. With store, good answers
// replace the cached copy.
async function networkFirst(req, key, { store = false, timeout = 0 } = {}) {
  const cache = await caches.open(APP_CACHE);
  let res = null;
  let failure = null;
  try {
    res = await fetchWithin(req, timeout);
  } catch (err) {
    failure = err;
  }
  if (res && !GATEWAY_ERRORS.has(res.status)) {
    if (store && res.ok && !res.redirected) {
      await cache.put(key, res.clone());
    }
    return res;
  }
  const hit = await cache.match(key);
  if (hit) {
    return hit;
  }
  if (res) {
    return res;
  }
  throw failure;
}

self.addEventListener("fetch", (e) => {
  const req = e.request;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) {
    return;
  }
  if (req.method === "POST" && SESSION_PATHS.has(url.pathname)) {
    e.waitUntil(caches.delete(DATA_CACHE));
    return;
  }
  if (req.method !== "GET" || url.pathname.startsWith("/api/")) {
    return;
  }
  if (req.mode === "navigate") {
    // Only the app page is kept; login, account and admin need the server.
    if (url.pathname === "/") {
      e.respondWith(networkFirst(req, "/", { store: true, timeout: NAVIGATE_TIMEOUT }));
    }
    return;
  }
  e.respondWith(networkFirst(req, url.pathname));
});
