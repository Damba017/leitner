// The main pages: overview, a heading, a list.

import { el, fill, icon } from "./ui.js";
import { languageName } from "./languages.js";
import { canSpeak, speak, spokenText } from "./speech.js";
import { practiceHash } from "./practice.js";
import { dueBadge } from "./tree.js";
import { ROOT } from "./state.js";

const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;

function title(text) {
  return el("h1", { tabindex: "-1", text });
}

// backLink goes one level up; only shown on phones (desktop has the crumbs).
function backLink(lib, parentId) {
  const parent = parentId ? lib.headings.get(parentId) : null;
  return el("a", { class: "back-link", href: parent ? `#/kop/${parent.id}` : "#/", text: `← ${parent ? parent.name : "Overzicht"}` });
}

// crumbs shows the path to the page: Overzicht › Spaans › Werkwoorden.
function crumbs(path, current) {
  return el(
    "nav",
    { class: "crumbs", "aria-label": "Kruimelpad" },
    el(
      "ol",
      {},
      el("li", {}, el("a", { href: "#/", text: "Overzicht" })),
      path.map((h) => el("li", {}, el("a", { href: `#/kop/${h.id}`, text: h.name }))),
      el("li", {}, el("span", { "aria-current": "page", text: current })),
    ),
  );
}

// pageHead is the top of a heading or list page: the way back, the title
// and the edit/delete buttons.
function pageHead(lib, parentId, name, tools) {
  return [
    backLink(lib, parentId),
    crumbs(lib.path(parentId), name),
    el("div", { class: "page-head" }, title(name), el("div", { class: "page-tools", "data-online": true }, tools)),
  ];
}

// button makes a button; focus names it, so focus can return to it after
// the page is drawn again (see focusKey in ui.js).
function button(text, onclick, { cls = "", focus = null, iconName = null, label = null } = {}) {
  return el(
    "button",
    { type: "button", class: cls || null, "data-focus": focus, "aria-label": label, title: label, onclick },
    iconName ? icon(iconName) : null,
    text ? el("span", { text }) : null,
  );
}

// practicePanel offers practicing a list or heading; due is the number of
// due questions in it (null while unknown).
function practicePanel(kind, id, due) {
  let summary;
  if (due === null) {
    summary = el("p", { class: "due-summary" }, el("span", { class: "due-label", text: "Oefenen" }));
  } else if (due === 0) {
    summary = el(
      "p",
      { class: "due-summary is-done" },
      icon("check"),
      el("span", { class: "due-label", text: "Alles is bij" }),
    );
  } else {
    summary = el("p", { class: "due-summary" }, el("span", { class: "due-number", text: String(due) }), el("span", { class: "due-label", text: "te doen" }));
  }
  return el(
    "section",
    { class: "practice-panel", "aria-label": "Oefenen" },
    summary,
    el(
      "div",
      { class: "row" },
      due === 0 ? null : el("a", { class: "button-link primary", href: practiceHash(kind, id, "due"), text: "Oefenen" }),
      el("a", { class: "button-link", href: practiceHash(kind, id, "free"), text: "Vrij oefenen" }),
    ),
  );
}

// headingTile is one top-level heading on the overview.
function headingTile(lib, id, due) {
  const heading = lib.headings.get(id);
  const c = lib.contents(id);
  const parts = [];
  if (c.lists) {
    parts.push(plural(c.lists, "lijst", "lijsten"));
  }
  if (c.headings) {
    parts.push(plural(c.headings, "subkop", "subkoppen"));
  }
  const n = due("heading", id);
  return el(
    "li",
    { class: "tile" },
    el("a", { class: "tile-link", href: `#/kop/${id}` }, icon("folder"), el("span", { text: heading.name })),
    el("p", { class: "tile-meta", text: parts.length ? parts.join(" · ") : "Nog leeg" }),
    el(
      "div",
      { class: "tile-foot" },
      n === null || !c.lists
        ? null
        : n
          ? el("span", { class: "tile-due" }, el("strong", { text: String(n) }), " te doen")
          : el("span", { class: "tile-due is-done" }, icon("check"), "Alles is bij"),
      n ? el("a", { class: "button-link primary small", href: practiceHash("heading", id, "due"), text: "Oefenen" }) : null,
    ),
  );
}

// renderHome is the overview: on desktop next to the tree (on phones the
// tree itself is the start screen). due(kind, id) as in renderTree.
export function renderHome(view, lib, actions, due) {
  const tops = lib.children(ROOT).filter((i) => i.kind === "heading");
  if (tops.length === 0) {
    fill(view,
      title("Overzicht"),
      el(
        "div",
        { class: "empty" },
        icon("cards"),
        el("p", { class: "empty-title", text: "Welkom bij Leitner" }),
        el("p", { text: "Begin met een kop, bijvoorbeeld een taal of een vak. Daarin maak je lijsten met kaarten." }),
        el("button", { type: "button", "data-online": true, onclick: () => actions.newHeading(null), text: "+ Nieuwe kop" }),
      ),
    );
    return;
  }
  const total = tops.reduce((n, h) => (n === null || due("heading", h.id) === null ? null : n + due("heading", h.id)), 0);
  fill(view,
    title("Overzicht"),
    el("p", {
      class: "lead",
      text: total === null ? "Kies een kop of lijst." : total ? `Er ${total === 1 ? "is" : "zijn"} ${total} te doen.` : "Alles is bij. Mooi zo!",
    }),
    el("ul", { class: "tiles" }, tops.map((h) => headingTile(lib, h.id, due))),
  );
}

// renderHeading shows a heading and what's in it; due(kind, id) as in renderTree.
export function renderHeading(view, lib, id, actions, due) {
  const heading = lib.headings.get(id);
  if (!heading) {
    renderNotFound(view);
    return;
  }
  const items = lib.children(id);
  fill(view,
    ...pageHead(lib, heading.parentId, heading.name, [
      button("Bewerken", () => actions.editHeading(id), { cls: "quiet", focus: "edit" }),
      button("Verwijderen", () => actions.deleteHeading(id), { cls: "quiet danger", focus: "delete" }),
    ]),
    lib.listsIn(id).length ? practicePanel("heading", id, due("heading", id)) : null,
    el(
      "div",
      { class: "section-head" },
      el("h2", { text: "Inhoud" }),
      el(
        "div",
        { class: "row", "data-online": true },
        button("Nieuwe lijst", () => actions.newList(id), { cls: "secondary", focus: "new-list", iconName: "plus" }),
        button("Nieuwe subkop", () => actions.newHeading(id), { cls: "secondary", focus: "new-heading", iconName: "plus" }),
      ),
    ),
    items.length
      ? el(
          "ul",
          { class: "contents" },
          items.map((item) => {
            const isHeading = item.kind === "heading";
            return el(
              "li",
              {},
              el(
                "a",
                { href: isHeading ? `#/kop/${item.id}` : `#/lijst/${item.id}` },
                icon(isHeading ? "folder" : "cards"),
                el("span", { class: "contents-name", text: lib.name(item) }),
                el("span", { class: "visually-hidden", text: isHeading ? " (kop)" : " (lijst)" }),
                dueBadge(due(item.kind, item.id)),
                icon("chevron"),
              ),
            );
          }),
        )
      : el("p", { class: "empty-note", text: "Deze kop is nog leeg. Voeg een lijst of subkop toe." }),
  );
}

// speakButton reads text aloud in lang; label says which side it is.
function speakButton(label, text, lang) {
  return el(
    "button",
    { type: "button", class: "icon-button", "aria-label": `${label} voorlezen`, title: "Voorlezen", onclick: () => speak(text, lang) },
    icon("speaker"),
  );
}

function cardRow(list, card, actions) {
  const side = (cls, lang, text, spoken, fallback) => {
    const label = languageName(lang) || fallback;
    return el(
      "div",
      { class: `card-side ${cls}` },
      el("span", { class: "card-label", text: label }),
      el("span", { class: "card-text" }, el("span", { lang: lang || null, text }), canSpeak ? speakButton(label, spoken, lang) : null),
    );
  };
  return el(
    "li",
    { class: "card-row" },
    side("side-1", list.lang1, card.front, card.pronunciation || spokenText(card.front), "Kant 1"),
    side("side-2", list.lang2, card.back, spokenText(card.back), "Kant 2"),
    card.note ? el("p", { class: "card-note" }, el("span", { class: "visually-hidden", text: "Notitie: " }), card.note) : null,
    el(
      "div",
      { class: "card-actions", "data-online": true },
      button(null, () => actions.editCard(card), { cls: "icon-button", focus: `card-edit-${card.id}`, iconName: "pencil", label: `„${card.front}” bewerken` }),
      button(null, () => actions.deleteCard(card), { cls: "icon-button danger", focus: `card-delete-${card.id}`, iconName: "trash", label: `„${card.front}” verwijderen` }),
    ),
  );
}

// listMeta sums up a list's settings in one line: how it is practiced and
// how strict the check is.
function listMeta(list) {
  const one = languageName(list.lang1);
  const two = languageName(list.lang2);
  const way = { "1-2": `${one} → ${two}`, "2-1": `${two} → ${one}`, both: `${one} ⇄ ${two}` }[list.direction] ?? list.direction;
  const checks = [
    ["hoofdletters", list.strictCase],
    ["leestekens", list.strictPunct],
    ["accenten", list.strictAccents],
  ];
  const strict = checks.filter(([, on]) => on).map(([name]) => name);
  const join = (names) => (names.length > 1 ? `${names.slice(0, -1).join(", ")} en ${names.at(-1)}` : names[0]);
  const check = strict.length
    ? `${join(strict)} ${strict.length === 1 ? "moet" : "moeten"} kloppen`
    : "hoofdletters, leestekens en accenten tellen niet mee";
  return el(
    "p",
    { class: "list-meta" },
    el("span", { title: "Oefenrichting", text: way }),
    el("span", { "aria-hidden": "true", text: " · " }),
    el("span", { text: check[0].toUpperCase() + check.slice(1) }),
  );
}

// renderList shows a list with its settings and cards; cards is null while
// they are loading, due the number of due questions (null while unknown).
export function renderList(view, lib, id, actions, cards, due) {
  const list = lib.lists.get(id);
  if (!list) {
    renderNotFound(view);
    return;
  }
  fill(view,
    ...pageHead(lib, list.headingId, list.name, [
      button("Bewerken", () => actions.editList(id), { cls: "quiet", focus: "edit" }),
      button("Verwijderen", () => actions.deleteList(id), { cls: "quiet danger", focus: "delete" }),
    ]),
    listMeta(list),
    cards?.length === 0 ? null : practicePanel("list", id, due),
    el(
      "div",
      { class: "section-head" },
      el("h2", {}, "Kaarten", cards ? el("span", { class: "h2-count", text: ` ${cards.length}` }) : null),
      el(
        "div",
        { class: "row", "data-online": true },
        button("Nieuwe kaart", () => actions.newCard(id), { cls: "secondary", focus: "new-card", iconName: "plus" }),
        button("Lijst plakken", () => actions.pasteCards(id), { cls: "quiet", focus: "paste" }),
        button("CSV importeren", () => actions.importCsv(id), { cls: "quiet", focus: "import" }),
        el("a", { class: "button-link quiet", href: `/api/lists/${id}/export.csv`, download: "", text: "CSV exporteren" }),
      ),
    ),
    cards === null
      ? el("p", { class: "empty-note", text: "Kaarten laden…" })
      : cards.length
        ? el("ul", { class: "cards" }, cards.map((c) => cardRow(list, c, actions)))
        : el(
            "div",
            { class: "empty" },
            icon("cards"),
            el("p", { class: "empty-title", text: "Nog geen kaarten" }),
            el("p", { "data-online": true, text: "Voeg ze één voor één toe, of plak een hele lijst in één keer." }),
            el(
              "div",
              { class: "row", "data-online": true },
              button("Nieuwe kaart", () => actions.newCard(id), { focus: "empty-new-card", iconName: "plus" }),
              button("Lijst plakken", () => actions.pasteCards(id), { cls: "secondary", focus: "empty-paste" }),
            ),
          ),
  );
}

export function renderNotFound(view) {
  fill(view,
    el("a", { class: "back-link", href: "#/", text: "← Overzicht" }),
    title("Niet gevonden"),
    el("p", { text: "Deze kop of lijst bestaat niet (meer)." }),
    el("p", {}, el("a", { href: "#/", text: "Naar het overzicht" })),
  );
}
