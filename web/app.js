// Entry point of the app (ES module): who is logged in, the tree, routing,
// offline mode. Routes are hashes, so they work offline too:
// #/ (overview), #/kop/{id} (a heading), #/lijst/{id} (a list),
// #/oefenen/kop/{id} and #/oefenen/lijst/{id} (practice what is due; with
// /vrij at the end: practice freely), #/kaart/{id}/{1-2|2-1} (the card of a
// notification, to answer) and #/meldingen (notification settings).
//
// Offline: when the server can't be reached (not just navigator.onLine), the
// app shows the snapshot kept on the device, read-only. It tries the server
// again on its own and whenever the app comes back to the foreground; once it
// answers, everything is fetched again.

import { api } from "./api.js";
import { isOffline, setOffline, saveSnapshot, loadSnapshot } from "./offline.js";
import { practiceItems, dueCounts, cardItem } from "./due.js";
import { showStatus, focusKey, refocus } from "./ui.js";
import { Library, ROOT, itemKey } from "./state.js";
import { renderTree, loadOpen, saveOpen, focusMoveButton, focusToggle } from "./tree.js";
import { renderHome, renderHeading, renderList, renderNotFound } from "./views.js";
import {
  setupDialogs,
  openHeadingDialog,
  openListDialog,
  openDeleteDialog,
  openCardDialog,
  openPasteDialog,
  openCsvDialog,
  openDeleteCardDialog,
} from "./dialogs.js";
import { makeSortable } from "./drag.js";
import { startPractice } from "./practice.js";
import { startNotify } from "./notify.js";

const app = document.getElementById("app");
const treeRoot = document.getElementById("tree");
const view = document.getElementById("view");
const orderToggle = document.getElementById("order-toggle");
const orderHint = document.getElementById("order-hint");

let lib = new Library({ headings: [], lists: [] });
const getLib = () => lib; // for views that outlive a reload of the tree
let open = loadOpen(); // expanded headings on this device
let ordering = false;
let saving = Promise.resolve(); // order changes go to the server one by one
let pendingStatus = null; // message for the page navigate() goes to
// The cards of the list on screen; null cards = not loaded yet.
let shown = { listId: null, cards: null };
let loadingFor = null; // list whose cards are being fetched
let due = null; // due questions per list id (Map), null = unknown
// The view on screen that draws itself (a practice session or the Meldingen
// tab): its route key and leave().
let session = null;
// The user's data as last kept on the device (GET /api/offline); used while
// offline, null if there is none.
let snapshot = null;
let retryTimer = 0;
let retryDelay = 0; // s; while offline the server is tried again after this

// showMe fills in who is logged in and shows the Admin link to admins.
function showMe(me) {
  document.getElementById("me").textContent = `Ingelogd als ${me.username}`;
  document.getElementById("admin-link").hidden = !me.isAdmin;
}

// setLibrary replaces the tree and forgets expanded headings that are gone.
function setLibrary(tree) {
  lib = new Library(tree);
  for (const id of open) {
    if (!lib.headings.has(id)) {
      open.delete(id);
    }
  }
  saveOpen(open);
}

function setOrdering(on) {
  ordering = on;
  orderToggle.setAttribute("aria-pressed", String(on));
  orderHint.hidden = !on;
  renderSidebar();
}

// goOffline switches to the kept snapshot, read-only, and plans the next try
// of the server. Only the switch itself redraws the page.
async function goOffline() {
  clearTimeout(retryTimer);
  retryDelay = Math.min(retryDelay ? retryDelay * 2 : 5, 60);
  retryTimer = setTimeout(reload, retryDelay * 1000);
  if (isOffline()) {
    return;
  }
  setOffline(true);
  if (ordering) {
    setOrdering(false);
  }
  // The one in memory is as new as the kept one, and there even if keeping it failed.
  snapshot = snapshot ?? (await loadSnapshot());
  if (snapshot) {
    setLibrary(snapshot);
    showMe(snapshot.me);
    showStatus("");
  } else {
    showStatus("Offline, en er staat nog niets op dit apparaat.", true);
  }
  shown = { listId: shown.listId, cards: null };
  due = snapshot ? dueCounts(snapshot, Date.now()) : null;
  revealCurrent();
  render();
}

// backOnline leaves offline mode; the caller fetches everything again.
function backOnline() {
  clearTimeout(retryTimer);
  retryDelay = 0;
  setOffline(false);
  showStatus("");
  shown = { listId: shown.listId, cards: null };
}

// refreshSnapshot keeps the user's current data on the device.
async function refreshSnapshot() {
  try {
    const data = await api("GET", "/api/offline");
    if (!isOffline()) {
      snapshot = data;
      await saveSnapshot(data);
    }
  } catch (err) {
    if (err.unreachable) {
      goOffline();
    }
  }
}

// offlineItems is loadItems for a practice session, from the snapshot.
function offlineItems(route) {
  if (route.kind === "card") {
    return { items: snapshot ? cardItem(snapshot, route.id, route.direction) : [], offline: true };
  }
  const ids = route.kind === "heading" ? lib.listsIn(route.id) : [route.id];
  return { items: snapshot ? practiceItems(snapshot, ids, route.mode, Date.now()) : [], offline: true };
}

// onlineItems fetches a session's questions; a card that is gone has none.
async function onlineItems(route) {
  if (route.kind === "card") {
    try {
      const { item } = await api("GET", `/api/cards/${route.id}?direction=${route.direction}`);
      return [item];
    } catch (err) {
      if (err.status === 404) {
        return [];
      }
      throw err;
    }
  }
  const param = route.kind === "heading" ? "heading" : "list";
  const { items } = await api("GET", `/api/practice?${param}=${route.id}&mode=${route.mode}`);
  return items;
}

// loadItems gives a practice session its questions: from the server, or
// from the snapshot when offline (then nothing is stored).
async function loadItems(route) {
  if (isOffline()) {
    return offlineItems(route);
  }
  try {
    return { items: await onlineItems(route), offline: false };
  } catch (err) {
    if (!err.unreachable) {
      throw err;
    }
    await goOffline();
    return offlineItems(route);
  }
}

function currentRoute() {
  if (location.hash === "#/meldingen") {
    return { kind: "notify" };
  }
  const card = location.hash.match(/^#\/kaart\/(\d+)\/(1-2|2-1)$/);
  if (card) {
    return { kind: "card", id: Number(card[1]), direction: card[2], practice: "due" };
  }
  const m = location.hash.match(/^#\/(oefenen\/)?(kop|lijst)\/(\d+)(\/vrij)?$/);
  if (!m || (m[4] && !m[1])) {
    return null;
  }
  const practice = m[1] ? (m[4] ? "free" : "due") : null;
  return { kind: m[2] === "kop" ? "heading" : "list", id: Number(m[3]), practice };
}

// dueCount is the number of due questions in a list or heading; null if unknown.
function dueCount(kind, id) {
  if (!due) {
    return null;
  }
  const ids = kind === "heading" ? lib.listsIn(id) : [id];
  return ids.reduce((n, listId) => n + (due.get(listId) ?? 0), 0);
}

// showDue redraws with new due counts. A practice session or the Meldingen
// tab draws itself; then only the tree is drawn again.
function showDue() {
  if (!session) {
    render();
    return;
  }
  const key = treeRoot.contains(document.activeElement) ? focusKey(document.activeElement) : null;
  renderSidebar();
  refocus(treeRoot, key);
}

// loadDue fetches the due counts and redraws (see showDue). Online it then
// also refreshes the snapshot: it runs after every change. Offline the
// counts come from the snapshot, at the current time.
async function loadDue() {
  if (isOffline()) {
    due = snapshot ? dueCounts(snapshot, Date.now()) : null;
    showDue();
    return;
  }
  try {
    const { lists } = await api("GET", "/api/due");
    due = new Map(Object.entries(lists).map(([id, n]) => [Number(id), n]));
  } catch (err) {
    if (err.unreachable) {
      goOffline();
      return;
    }
    due = null; // the buttons then show no number
  }
  showDue();
  refreshSnapshot();
}

// revealCurrent expands the headings above the open page.
function revealCurrent() {
  const r = currentRoute();
  if (r?.kind !== "heading" && r?.kind !== "list") {
    return;
  }
  const parent = r.kind === "heading" ? lib.headings.get(r.id)?.parentId : lib.lists.get(r.id)?.headingId;
  for (const h of lib.path(parent)) {
    open.add(h.id);
  }
  saveOpen(open);
}

function renderSidebar() {
  renderTree(treeRoot, lib, { current: currentRoute(), open, ordering, due: dueCount, onToggle: toggle, onMove: moveBy });
}

// markMenu marks the top-bar link of the page on screen.
function markMenu(notify) {
  for (const a of document.querySelectorAll(".menu a[href^='#']")) {
    if ((a.getAttribute("href") === "#/meldingen") === notify) {
      a.setAttribute("aria-current", "page");
    } else {
      a.removeAttribute("aria-current");
    }
  }
}

// render draws the sidebar and the page; focus moves to the page title, for
// keyboard and screen reader users after navigating. A redraw of the same
// page (new due counts, back from the background, after a dialog) keeps
// focus on the same control.
function render({ focus = false } = {}) {
  const active = document.activeElement;
  const key = focus ? null : focusKey(active);
  const where = view.contains(active) ? view : treeRoot.contains(active) ? treeRoot : null;
  draw(focus);
  if (where) {
    refocus(where, key);
  }
}

function draw(focus) {
  const r = currentRoute();
  app.dataset.route = r ? (r.practice ? "practice" : r.kind) : "home";
  markMenu(r?.kind === "notify");
  renderSidebar();
  const key = r?.practice || r?.kind === "notify" ? location.hash : null;
  if (session && session.key !== key) {
    session.leave();
    session = null;
  }
  if (r?.kind === "notify") {
    // The tab draws itself, like a session.
    if (!session) {
      session = { key, leave: startNotify(view, getLib) };
    }
    if (focus) {
      window.scrollTo(0, 0);
    }
    return;
  }
  if (r?.practice) {
    // A session draws itself; redraws of the sidebar leave it alone. A
    // card's list is only known once the card is loaded.
    const exists = r.kind === "card" || (r.kind === "heading" ? lib.headings : lib.lists).has(r.id);
    if (!session && exists) {
      const route = { kind: r.kind, id: r.id, mode: r.practice, direction: r.direction };
      session = { key, leave: startPractice(view, getLib, route, { loadItems, onEnd: loadDue, onUnreachable: goOffline }) };
    } else if (!exists) {
      renderNotFound(view);
    }
    if (focus) {
      window.scrollTo(0, 0);
    }
    return;
  }
  if (r?.kind === "heading") {
    renderHeading(view, lib, r.id, actions, dueCount);
  } else if (r?.kind === "list") {
    if (shown.listId !== r.id) {
      shown = { listId: r.id, cards: null };
    }
    if (shown.cards === null && isOffline()) {
      shown.cards = snapshot ? snapshot.cards.filter((c) => c.listId === r.id) : [];
    }
    renderList(view, lib, r.id, actions, shown.cards, dueCount("list", r.id));
    if (shown.cards === null && lib.lists.has(r.id)) {
      loadCards(r.id);
    }
  } else {
    renderHome(view, lib, actions, dueCount);
  }
  if (focus) {
    (r ? view.querySelector("h1") : document.getElementById("sidebar-title"))?.focus({ preventScroll: true });
    window.scrollTo(0, 0);
  }
}

// loadCards fetches the cards of a list and shows them if that list is still
// the open page. Errors are shown, never thrown.
async function loadCards(listId) {
  if (loadingFor === listId) {
    return;
  }
  loadingFor = listId;
  try {
    const cards = await api("GET", `/api/lists/${listId}/cards`);
    if (shown.listId === listId) {
      shown.cards = cards;
      render();
    }
  } catch (err) {
    if (err.unreachable) {
      goOffline(); // shows the kept cards
      return;
    }
    showStatus(err.message, true);
    if (shown.listId === listId) {
      shown.cards = []; // stop retrying; a reload of the page tries again
      render();
    }
  } finally {
    loadingFor = null;
  }
}

// refreshCards fetches the open list's cards (and the due counts) again
// after a change.
function refreshCards(listId) {
  shown = { listId, cards: shown.listId === listId ? shown.cards : null };
  loadingFor = null;
  loadDue();
  return loadCards(listId);
}

// reload fetches who is logged in and the tree again; errors are shown,
// never thrown. It is also the offline check: unreachable means offline, an
// answer ends offline mode.
async function reload() {
  let me, tree;
  try {
    [me, tree] = await Promise.all([api("GET", "/api/me"), api("GET", "/api/tree")]);
  } catch (err) {
    if (err.unreachable) {
      await goOffline();
      return;
    }
    showStatus(err.message, true);
    render();
    return;
  }
  if (isOffline()) {
    backOnline();
  }
  showMe(me);
  setLibrary(tree);
  render();
  loadDue();
}

// navigate goes to hash and shows message there.
function navigate(hash, message = "") {
  if (location.hash === hash || (hash === "#/" && !location.hash)) {
    showStatus(message);
    revealCurrent();
    render({ focus: true });
  } else {
    pendingStatus = message;
    location.hash = hash;
  }
}

window.addEventListener("hashchange", () => {
  showStatus(pendingStatus ?? "");
  pendingStatus = null;
  revealCurrent();
  render({ focus: true });
});

function toggle(id) {
  if (!open.delete(id)) {
    open.add(id);
  }
  saveOpen(open);
  renderSidebar();
  focusToggle(treeRoot, id);
}

// applyOrder shows the new order right away and then saves it. If saving
// fails (another device changed the heading), the server's tree comes back.
function applyOrder(parent, items) {
  lib.setOrder(parent, items);
  render();
  const body = { parentId: parent === ROOT ? null : parent, items: items.map(({ kind, id }) => ({ kind, id })) };
  saving = saving
    .then(() => api("PUT", "/api/order", body))
    .catch(async (err) => {
      showStatus(err.message, true);
      await reload();
    });
}

function moveBy(parent, key, step) {
  const items = [...lib.children(parent)];
  const from = items.findIndex((i) => itemKey(i) === key);
  const to = from + step;
  if (from < 0 || to < 0 || to >= items.length) {
    return;
  }
  [items[from], items[to]] = [items[to], items[from]];
  applyOrder(parent, items);
  focusMoveButton(treeRoot, key, step);
}

const parentOf = (container) => (container.dataset.parent === ROOT ? ROOT : Number(container.dataset.parent));

const actions = {
  newHeading(parentId) {
    openHeadingDialog(lib, { parentId }, async (h) => {
      await reload();
      navigate(`#/kop/${h.id}`, `Kop „${h.name}” gemaakt.`);
    });
  },
  newList(headingId) {
    openListDialog(lib, { headingId }, async (l) => {
      await reload();
      navigate(`#/lijst/${l.id}`, `Lijst „${l.name}” gemaakt.`);
    });
  },
  editHeading(id) {
    openHeadingDialog(lib, { heading: lib.headings.get(id) }, async () => {
      await reload();
      navigate(`#/kop/${id}`, "Opgeslagen.");
    });
  },
  editList(id) {
    openListDialog(lib, { list: lib.lists.get(id) }, async () => {
      await reload();
      navigate(`#/lijst/${id}`, "Opgeslagen.");
    });
  },
  newCard(listId) {
    openCardDialog(lib.lists.get(listId), {}, () => refreshCards(listId));
  },
  editCard(card) {
    openCardDialog(lib.lists.get(card.listId), { card }, async () => {
      await refreshCards(card.listId);
      showStatus("Opgeslagen.");
    });
  },
  deleteCard(card) {
    openDeleteCardDialog(card, async () => {
      await refreshCards(card.listId);
      showStatus("Kaart verwijderd.");
    });
  },
  pasteCards(listId) {
    openPasteDialog(lib.lists.get(listId), async ({ added }) => {
      await refreshCards(listId);
      showStatus(`${added} ${added === 1 ? "kaart" : "kaarten"} toegevoegd.`);
    });
  },
  importCsv(listId) {
    openCsvDialog(lib.lists.get(listId), async ({ added }) => {
      await refreshCards(listId);
      showStatus(`${added} ${added === 1 ? "kaart" : "kaarten"} geïmporteerd.`);
    });
  },
  deleteHeading(id) {
    const { name, parentId } = lib.headings.get(id);
    openDeleteDialog(lib, { kind: "heading", id }, async () => {
      await reload();
      navigate(parentId ? `#/kop/${parentId}` : "#/", `Kop „${name}” verwijderd.`);
    });
  },
  deleteList(id) {
    const { name, headingId } = lib.lists.get(id);
    openDeleteDialog(lib, { kind: "list", id }, async () => {
      await reload();
      navigate(`#/kop/${headingId}`, `Lijst „${name}” verwijderd.`);
    });
  },
};

setupDialogs();
document.getElementById("add-top").addEventListener("click", () => actions.newHeading(null));
// The skip link can't use its href: hashes are routes here.
document.getElementById("skip-link").addEventListener("click", (e) => {
  e.preventDefault();
  const target = getComputedStyle(view).display === "none" ? document.getElementById("sidebar-title") : view.querySelector("h1");
  target?.focus();
});
orderToggle.addEventListener("click", () => setOrdering(!ordering));
makeSortable(treeRoot, {
  itemSelector: ".tree-item",
  handleSelector: ".drag-handle",
  onReorder(container, keys) {
    const parent = parentOf(container);
    const byKey = new Map(lib.children(parent).map((i) => [itemKey(i), i]));
    applyOrder(parent, keys.map((k) => byKey.get(k)).filter(Boolean));
  },
});

// Back in the foreground (iOS keeps a closed app around), or the network
// is back: check the server and fetch everything again. Offline that also
// brings the due counts up to date.
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState === "visible") {
    if (isOffline()) {
      loadDue();
    }
    reload();
  }
});
window.addEventListener("pageshow", (e) => {
  if (e.persisted) {
    reload(); // restored from the back/forward cache
  }
});
window.addEventListener("online", reload);
window.addEventListener("offline", goOffline);

// A click on a notification while the app is open: the service worker asks
// this page to show the card when it can't navigate it itself, or to start
// it again when it is already on screen (e.g. finished an hour ago).
if ("serviceWorker" in navigator) {
  navigator.serviceWorker.addEventListener("message", (e) => {
    if (e.data?.type !== "open" || typeof e.data.url !== "string") {
      return;
    }
    const url = new URL(e.data.url, location.href);
    if (url.origin !== location.origin) {
      return;
    }
    if (url.href !== location.href) {
      location.assign(url.href);
      return;
    }
    if (session) {
      session.leave();
      session = null;
    }
    render({ focus: true });
  });
  navigator.serviceWorker.startMessages?.();
}

(navigator.onLine === false ? goOffline() : reload()).then(revealCurrent).then(() => render());
