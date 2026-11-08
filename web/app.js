// Entry point of the app shell (ES module). The real UI starts in phase 3.

// showMe fills in who is logged in and shows the Admin link to admins.
async function showMe() {
  const res = await fetch("/api/me", { headers: { Accept: "application/json" } });
  if (res.status === 401) {
    location.href = "/login";
    return;
  }
  if (!res.ok) {
    throw new Error(`/api/me: ${res.status}`);
  }
  const me = await res.json();
  document.getElementById("me").textContent = `Ingelogd als ${me.username}`;
  document.getElementById("admin-link").hidden = !me.isAdmin;
}

showMe().catch((err) => console.error(err));
