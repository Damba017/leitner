// The main pages: overview, a heading, a list.

import { el } from "./ui.js";
import { languageName } from "./languages.js";
import { canSpeak, speak, spokenText } from "./speech.js";
import { practiceHash } from "./practice.js";

const directions = {
  "1-2": "Kant 1 → kant 2",
  "2-1": "Kant 2 → kant 1",
  both: "Beide (elke keer een willekeurige kant)",
};

function title(text, icon) {
  return el("h1", { tabindex: "-1" }, icon ? el("span", { "aria-hidden": "true", text: `${icon} ` }) : null, text);
}

const backLink = () => el("a", { class: "back-link", href: "#/", text: "← Overzicht" });

// crumbs shows the path to the page: Overzicht › Spaans › Werkwoorden.
function crumbs(path, current) {
  return el(
    "nav",
    { "aria-label": "Kruimelpad" },
    el(
      "ol",
      { class: "crumbs" },
      el("li", {}, el("a", { href: "#/", text: "Overzicht" })),
      path.map((h) => el("li", {}, el("a", { href: `#/kop/${h.id}`, text: h.name }))),
      el("li", {}, el("span", { "aria-current": "page", text: current })),
    ),
  );
}

// practiceRow links to practicing a list or heading; due is the number of
// due questions in it (null while unknown).
function practiceRow(kind, id, due) {
  const label = due === null ? "Oefenen" : `Oefenen (${due} te doen)`;
  return el(
    "div",
    { class: "row actions" },
    due === 0
      ? el("span", { class: "muted", text: "Niets te doen." })
      : el("a", { class: "button-link primary", href: practiceHash(kind, id, "due"), text: label }),
    el("a", { class: "button-link", href: practiceHash(kind, id, "free"), text: "Vrij oefenen" }),
  );
}

export function renderHome(view, lib, actions) {
  const empty = lib.headings.size === 0;
  view.replaceChildren(
    title("Overzicht"),
    el("p", {
      text: empty
        ? "Je hebt nog geen koppen. Begin met een kop, bijvoorbeeld een taal of een vak."
        : "Kies links een kop of lijst.",
    }),
    ...(empty
      ? [el("button", { type: "button", onclick: () => actions.newHeading(null), text: "+ Nieuwe kop" })]
      : [
          el("h2", { text: "Back-up" }),
          el("p", { class: "hint", text: "Alle koppen, lijsten en kaarten in één JSON-bestand." }),
          el("p", {}, el("a", { class: "button-link", href: "/api/export.json", download: "", text: "Alles downloaden (JSON)" })),
        ]),
  );
}

export function renderHeading(view, lib, id, actions, due) {
  const heading = lib.headings.get(id);
  if (!heading) {
    renderNotFound(view);
    return;
  }
  const items = lib.children(id);
  view.replaceChildren(
    backLink(),
    crumbs(lib.path(heading.parentId), heading.name),
    title(heading.name, "📂"),
    practiceRow("heading", id, due),
    el(
      "div",
      { class: "row actions" },
      el("button", { type: "button", onclick: () => actions.newList(id), text: "+ Nieuwe lijst" }),
      el("button", { type: "button", class: "secondary", onclick: () => actions.newHeading(id), text: "+ Nieuwe subkop" }),
      el("button", { type: "button", class: "secondary", onclick: () => actions.editHeading(id), text: "Bewerken" }),
      el("button", { type: "button", class: "danger", onclick: () => actions.deleteHeading(id), text: "Verwijderen" }),
    ),
    items.length
      ? el(
          "ul",
          { class: "contents" },
          items.map((item) =>
            el(
              "li",
              {},
              el(
                "a",
                { href: item.kind === "heading" ? `#/kop/${item.id}` : `#/lijst/${item.id}` },
                el("span", { "aria-hidden": "true", text: item.kind === "heading" ? "📁" : "📄" }),
                el("span", { text: lib.name(item) }),
                el("span", { class: "visually-hidden", text: item.kind === "heading" ? " (kop)" : " (lijst)" }),
              ),
            ),
          ),
        )
      : el("p", { text: "Deze kop is nog leeg. Voeg een lijst of subkop toe." }),
  );
}

// speakButton reads text aloud in lang; label says which side it is.
function speakButton(label, text, lang) {
  return el("button", {
    type: "button",
    class: "icon secondary",
    "aria-label": `${label} voorlezen`,
    onclick: () => speak(text, lang),
    text: "🔊",
  });
}

function cardRow(list, card, actions) {
  const side = (label, text, lang, spoken) =>
    el(
      "div",
      { class: "card-side" },
      el("span", { class: "card-label", text: label }),
      el("span", { class: "card-text", lang, text }),
      canSpeak ? speakButton(label, spoken, lang) : null,
    );
  return el(
    "li",
    { class: "card-row" },
    side("Kant 1", card.front, list.lang1, card.pronunciation || spokenText(card.front)),
    side("Kant 2", card.back, list.lang2, spokenText(card.back)),
    card.note ? el("p", { class: "card-note" }, el("span", { class: "visually-hidden", text: "Notitie: " }), card.note) : null,
    el(
      "div",
      { class: "row card-actions" },
      el("button", { type: "button", class: "secondary small", onclick: () => actions.editCard(card), text: "Bewerken" }),
      el("button", { type: "button", class: "danger small", onclick: () => actions.deleteCard(card), text: "Verwijderen" }),
    ),
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
  const strict = (on) => (on ? "moeten kloppen" : "tellen niet mee");
  view.replaceChildren(
    backLink(),
    crumbs(lib.path(list.headingId), list.name),
    title(list.name, "📄"),
    practiceRow("list", id, due),
    el(
      "div",
      { class: "row actions" },
      el("button", { type: "button", class: "secondary", onclick: () => actions.editList(id), text: "Bewerken" }),
      el("button", { type: "button", class: "danger", onclick: () => actions.deleteList(id), text: "Verwijderen" }),
    ),
    el(
      "dl",
      { class: "settings" },
      el("dt", { text: "Kant 1" }),
      el("dd", { text: languageName(list.lang1) }),
      el("dt", { text: "Kant 2" }),
      el("dd", { text: languageName(list.lang2) }),
      el("dt", { text: "Oefenrichting" }),
      el("dd", { text: directions[list.direction] ?? list.direction }),
      el("dt", { text: "Hoofdletters" }),
      el("dd", { text: strict(list.strictCase) }),
      el("dt", { text: "Leestekens" }),
      el("dd", { text: strict(list.strictPunct) }),
      el("dt", { text: "Accenten" }),
      el("dd", { text: strict(list.strictAccents) }),
    ),
    el("h2", { text: cards ? `Kaarten (${cards.length})` : "Kaarten" }),
    el(
      "div",
      { class: "row actions" },
      el("button", { type: "button", onclick: () => actions.newCard(id), text: "+ Nieuwe kaart" }),
      el("button", { type: "button", class: "secondary", onclick: () => actions.pasteCards(id), text: "Lijst plakken" }),
      el("button", { type: "button", class: "secondary", onclick: () => actions.importCsv(id), text: "CSV importeren" }),
      el("a", { class: "button-link", href: `/api/lists/${id}/export.csv`, download: "", text: "CSV exporteren" }),
    ),
    cards === null
      ? el("p", { text: "Kaarten laden…" })
      : cards.length
        ? el("ul", { class: "cards" }, cards.map((c) => cardRow(list, c, actions)))
        : el("p", { text: "Deze lijst heeft nog geen kaarten." }),
  );
}

export function renderNotFound(view) {
  view.replaceChildren(
    backLink(),
    title("Niet gevonden"),
    el("p", { text: "Deze kop of lijst bestaat niet (meer)." }),
    el("p", {}, el("a", { href: "#/", text: "Naar het overzicht" })),
  );
}
