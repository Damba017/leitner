// Theme per device: "light", "dark" or nothing (follow the system).
// A classic script in <head>, not a module: it runs before the first paint,
// so a saved theme never flashes the other one first. It's an external file
// because the CSP allows no inline scripts. Used by the app and the
// server-rendered pages alike.
(function () {
  const key = "leitner.theme";
  const themes = ["light", "dark"];

  function saved() {
    try {
      const t = localStorage.getItem(key);
      return themes.includes(t) ? t : "";
    } catch {
      return ""; // storage blocked (private mode): follow the system
    }
  }

  // The browser's bar colour per theme: the top bar's (--surface in app.css).
  const barColors = { light: "#ffffff", dark: "#1d2523" };

  function apply(theme) {
    if (theme) {
      document.documentElement.dataset.theme = theme;
    } else {
      delete document.documentElement.dataset.theme;
    }
    // The <meta name="theme-color"> tags follow the system; a chosen theme
    // overrides both.
    for (const meta of document.querySelectorAll('meta[name="theme-color"]')) {
      const system = meta.media.includes("dark") ? "dark" : "light";
      meta.content = barColors[theme || system];
    }
  }

  apply(saved());

  document.addEventListener("DOMContentLoaded", () => {
    const select = document.getElementById("theme");
    if (!select) {
      return;
    }
    select.value = saved() || "system";
    select.addEventListener("change", () => {
      const theme = themes.includes(select.value) ? select.value : "";
      apply(theme);
      try {
        if (theme) {
          localStorage.setItem(key, theme);
        } else {
          localStorage.removeItem(key);
        }
      } catch {
        // Not saved; it still applies until the page is closed.
      }
    });
  });
})();
