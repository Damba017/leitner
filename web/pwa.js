// Registers the service worker. A classic script, loaded by the app and the
// server-rendered pages alike; without service worker support it does nothing.
if ("serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("/sw.js", { scope: "/" }).catch(() => {
      // The app works without it; nothing to tell the user.
    });
  });
}
