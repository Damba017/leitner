// The main pages: overview, a heading, a list.

import { el } from "./ui.js";
import { createCard } from "./card.js";
import { languageName } from "./languages.js";

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

export function renderHome(view, lib, actions) {
  const empty = lib.headings.size === 0;
  view.replaceChildren(
    title("Overzicht"),
    el("p", {
      text: empty
        ? "Je hebt nog geen koppen. Begin met een kop, bijvoorbeeld een taal of een vak."
        : "Kies links een kop of lijst.",
    }),
    ...(empty ? [el("button", { type: "button", onclick: () => actions.newHeading(null), text: "+ Nieuwe kop" })] : []),
  );
}

export function renderHeading(view, lib, id, actions) {
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

export function renderList(view, lib, id, actions) {
  const list = lib.lists.get(id);
  if (!list) {
    renderNotFound(view);
    return;
  }
  const strict = (on) => (on ? "moeten kloppen" : "tellen niet mee");
  const lang1 = languageName(list.lang1);
  const lang2 = languageName(list.lang2);
  // Temporary: shows the card component until phase 4 brings real cards.
  const sample = createCard({
    front: { text: "Kant 1", lang: list.lang1, label: lang1 },
    back: { text: "Kant 2", lang: list.lang2, label: lang2 },
  });
  view.replaceChildren(
    backLink(),
    crumbs(lib.path(list.headingId), list.name),
    title(list.name, "📄"),
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
      el("dd", { text: lang1 }),
      el("dt", { text: "Kant 2" }),
      el("dd", { text: lang2 }),
      el("dt", { text: "Oefenrichting" }),
      el("dd", { text: directions[list.direction] ?? list.direction }),
      el("dt", { text: "Hoofdletters" }),
      el("dd", { text: strict(list.strictCase) }),
      el("dt", { text: "Leestekens" }),
      el("dd", { text: strict(list.strictPunct) }),
      el("dt", { text: "Accenten" }),
      el("dd", { text: strict(list.strictAccents) }),
    ),
    el("h2", { text: "Kaarten" }),
    el("p", { text: "Kaarten toevoegen komt in de volgende versie. Zo ziet een kaart eruit:" }),
    sample.element,
    el("p", { class: "hint", text: "Klik op de kaart, of druk op Enter of spatie, om hem om te draaien." }),
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
