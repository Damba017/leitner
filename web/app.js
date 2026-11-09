// Entry point of the app (ES module): who is logged in, the tree, routing.
// Routes are hashes, so they keep working offline later:
// #/ (overview), #/kop/{id} (a heading), #/lijst/{id} (a list),
// #/oefenen/kop/{id} and #/oefenen/lijst/{id} (practice what is due; with
// /vrij at the end: practice freely).

import { api } from "./api.js";
import { showStatus } from "./ui.js";
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

const app = document.getElementById("app");
const treeRoot = document.getElementById("tree");
const view = document.getElementById("view");
const orderToggle = document.getElementById("order-toggle");
const orderHint = document.getElementById("order-hint");

let lib = new Library({ headings: [], lists: [] });
let open = loadOpen(); // expanded headings on this device
let ordering = false;
let saving = Promise.resolve(); // order changes go to the server one by one
let pendingStatus = null; // message for the page navigate() goes to
// The cards of the list on screen; null cards = not loaded yet.
let shown = { listId: null, cards: null };
let loadingFor = null; // list whose cards are being fetched
let due = null; // due questions per list id (Map), null = unknown
// The practice session on screen: its route key and leave().
let session = null;

// showMe fills in who is logged in and shows the Admin link to admins.
async function showMe() {
  const me = await api("GET", "/api/me");
  document.getElementById("me").textContent = `Ingelogd als ${me.username}`;
  document.getElementById("admin-link").hidden = !me.isAdmin;
}

function currentRoute() {
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

// loadDue fetches the due counts and redraws, except during practice.
async function loadDue() {
  try {
    const { lists } = await api("GET", "/api/due");
    due = new Map(Object.entries(lists).map(([id, n]) => [Number(id), n]));
  } catch {
    due = null; // the buttons then show no number
  }
  if (!session) {
    render();
  }
}

// revealCurrent expands the headings above the open page.
function revealCurrent() {
  const r = currentRoute();
  if (!r) {
    return;
  }
  const parent = r.kind === "heading" ? lib.headings.get(r.id)?.parentId : lib.lists.get(r.id)?.headingId;
  for (const h of lib.path(parent)) {
    open.add(h.id);
  }
  saveOpen(open);
}

function renderSidebar() {
  renderTree(treeRoot, lib, { current: currentRoute(), open, ordering, onToggle: toggle, onMove: moveBy });
}

// render draws the sidebar and the page; focus moves to the page title, for
// keyboard and screen reader users after navigating.
function render({ focus = false } = {}) {
  const r = currentRoute();
  app.dataset.route = r ? (r.practice ? "practice" : r.kind) : "home";
  renderSidebar();
  const key = r?.practice ? location.hash : null;
  if (session && session.key !== key) {
    session.leave();
    session = null;
  }
  if (r?.practice) {
    // A session draws itself; redraws of the sidebar leave it alone.
    const exists = (r.kind === "heading" ? lib.headings : lib.lists).has(r.id);
    if (!session && exists) {
      session = { key, leave: startPractice(view, lib, { kind: r.kind, id: r.id, mode: r.practice }, loadDue) };
    } else if (!exists) {
      renderNotFound(view);
    }
    if (focus) {
      window.scrollTo(0, 0);
    }
    return;
  }
  if (r?.kind === "heading") {
    renderHeading(view, lib, r.id, actions, dueCount("heading", r.id));
  } else if (r?.kind === "list") {
    if (shown.listId !== r.id) {
      shown = { listId: r.id, cards: null };
    }
    renderList(view, lib, r.id, actions, shown.cards, dueCount("list", r.id));
    if (shown.cards === null && lib.lists.has(r.id)) {
      loadCards(r.id);
    }
  } else {
    renderHome(view, lib, actions);
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

// reload fetches the tree again; errors are shown, never thrown.
async function reload() {
  try {
    lib = new Library(await api("GET", "/api/tree"));
    for (const id of open) {
      if (!lib.headings.has(id)) {
        open.delete(id);
      }
    }
    saveOpen(open);
  } catch (err) {
    showStatus(err.message, true);
  }
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
orderToggle.addEventListener("click", () => {
  ordering = !ordering;
  orderToggle.setAttribute("aria-pressed", String(ordering));
  orderHint.hidden = !ordering;
  renderSidebar();
});
makeSortable(treeRoot, {
  itemSelector: ".tree-item",
  handleSelector: ".drag-handle",
  onReorder(container, keys) {
    const parent = parentOf(container);
    const byKey = new Map(lib.children(parent).map((i) => [itemKey(i), i]));
    applyOrder(parent, keys.map((k) => byKey.get(k)).filter(Boolean));
  },
});

showMe().catch((err) => showStatus(err.message, true));
reload().then(revealCurrent).then(() => render());
