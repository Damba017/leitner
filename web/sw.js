// Service worker: keeps the app shell available offline and turns push
// messages into notifications. The server puts VERSION and APP_FILES in front
// of this file (sw.go).
//
// - App files: cached at install in a cache named after VERSION; the network
//   wins while it answers, so a new build shows up on the next load.
// - The API goes straight to the network: the page keeps its own snapshot of
//   the user's data (offline.js), so it always knows when it is offline.
// - Logging in or out drops that snapshot: it belongs to one user.
// - A push always shows a notification; a click opens its card in the app.

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

// showPush shows the notification of a push message ({title, body, url,
// tag}). It never rejects: Safari takes the push permission away from a site
// that receives a push without showing one, so anything odd (no JSON, an
// option the browser refuses) still ends in a plain notification.
async function showPush(data) {
  let msg = {};
  try {
    msg = data ? data.json() : {};
  } catch {
    // Not JSON: the plain notification below.
  }
  if (!msg || typeof msg !== "object") {
    msg = {};
  }
  const title = typeof msg.title === "string" && msg.title ? msg.title : "Leitner";
  const options = {
    body: typeof msg.body === "string" ? msg.body : "",
    icon: "/icons/icon-192.png",
    data: { url: typeof msg.url === "string" ? msg.url : "/" },
  };
  if (typeof msg.tag === "string" && msg.tag) {
    // Replaces the previous notification, and still alerts (renotify).
    options.tag = msg.tag;
    options.renotify = true;
  }
  try {
    await self.registration.showNotification(title, options);
  } catch {
    await self.registration.showNotification(title, { body: options.body, data: options.data });
  }
}

self.addEventListener("push", (e) => {
  e.waitUntil(showPush(e.data));
});

// appURL is the page a notification opens: its own path on this site, else
// the start page.
function appURL(notification) {
  const raw = (notification.data && notification.data.url) || "/";
  let url;
  try {
    url = new URL(raw, self.location.origin);
  } catch {
    url = null;
  }
  return url && url.origin === self.location.origin ? url.href : new URL("/", self.location.origin).href;
}

self.addEventListener("notificationclick", (e) => {
  e.notification.close();
  e.waitUntil(openApp(appURL(e.notification)));
});

// openApp shows url in an open Leitner window (focused) or a new one. The
// card routes differ only in the hash, so a window that can't be navigated is
// asked to go there itself, and one that is already there to start the card
// again (app.js listens).
async function openApp(url) {
  const wins = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
  const win = wins.find((c) => c.url === url) || wins[0];
  if (!win) {
    await self.clients.openWindow(url);
    return;
  }
  try {
    await win.focus();
  } catch {
    // Focus is only allowed during the click; the page still moves below.
  }
  if (win.url === url) {
    win.postMessage({ type: "open", url });
    return;
  }
  try {
    if (await win.navigate(url)) {
      return;
    }
  } catch {
    // Not a page this worker controls, or no navigate(): ask the page.
  }
  win.postMessage({ type: "open", url });
}
