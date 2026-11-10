// Offline read-only mode. The app decides when (app.js: the server can't be
// reached); CSS hides the edit controls under html[data-offline]. The
// snapshot of the user's data (GET /api/offline) is kept in the Cache API,
// so the app can open and practice without the server. The service worker
// drops it when someone logs in or out (sw.js).

export const DATA_CACHE = "leitner-data";
const SNAPSHOT_KEY = "/api/offline";

export const isOffline = () => document.documentElement.hasAttribute("data-offline");

export function setOffline(offline) {
  document.documentElement.toggleAttribute("data-offline", offline);
  document.getElementById("offline-banner").hidden = !offline;
}

// saveSnapshot keeps data on the device. Failing (no Cache API, storage
// full) only means no offline use; the app works on.
export async function saveSnapshot(data) {
  try {
    const cache = await caches.open(DATA_CACHE);
    await cache.put(SNAPSHOT_KEY, new Response(JSON.stringify(data), { headers: { "Content-Type": "application/json" } }));
  } catch {
    // Nothing to tell the user while online.
  }
}

// loadSnapshot returns the kept data, or null if there is none.
export async function loadSnapshot() {
  try {
    const cache = await caches.open(DATA_CACHE);
    const res = await cache.match(SNAPSHOT_KEY);
    return res ? await res.json() : null;
  } catch {
    return null;
  }
}

// dropSnapshot removes the kept data (logged out).
export async function dropSnapshot() {
  try {
    await caches.delete(DATA_CACHE);
  } catch {
    // No Cache API: nothing was kept.
  }
}
