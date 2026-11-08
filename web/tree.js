// The sidebar tree: headings (collapsible) and lists in the user's order.
// In ordering mode every row gets a drag handle and up/down buttons.

import { el } from "./ui.js";
import { ROOT, itemKey } from "./state.js";

const openKey = "leitner.open";

// loadOpen returns the ids of the expanded headings on this device.
export function loadOpen() {
  try {
    const ids = JSON.parse(localStorage.getItem(openKey) || "[]");
    return new Set(Array.isArray(ids) ? ids.filter(Number.isInteger) : []);
  } catch {
    return new Set();
  }
}

export function saveOpen(open) {
  try {
    localStorage.setItem(openKey, JSON.stringify([...open]));
  } catch {
    // Not saved: the tree just opens folded next time.
  }
}

// renderTree draws lib into root. current is the open page ({kind, id} or
// null); open is the set of expanded heading ids. onToggle(id) folds a
// heading; onMove(parent, key, step) moves an item up (-1) or down (+1).
export function renderTree(root, lib, { current, open, ordering, onToggle, onMove }) {
  const isCurrent = (item) => current && current.kind === item.kind && current.id === item.id;

  const level = (parent) => {
    const items = lib.children(parent);
    const ul = el("ul", { class: "tree-list", "data-parent": parent });
    items.forEach((item, i) => {
      const name = lib.name(item);
      const here = isCurrent(item);
      const row = el("div", { class: here ? "tree-row is-current" : "tree-row" });
      if (ordering) {
        row.append(el("span", { class: "drag-handle", "aria-hidden": "true", title: "Slepen", text: "⠿" }));
      }
      const li = el("li", { class: "tree-item", "data-key": itemKey(item) }, row);
      if (item.kind === "heading") {
        const isOpen = open.has(item.id);
        row.append(
          el("button", {
            type: "button",
            class: "tree-toggle",
            "aria-expanded": String(isOpen),
            "aria-label": `Inhoud van ${name}`,
            onclick: () => onToggle(item.id),
            text: isOpen ? "▾ 📂" : "▸ 📁",
          }),
          el("a", { class: "tree-link", href: `#/kop/${item.id}`, "aria-current": here && "page", text: name }),
        );
        if (isOpen) {
          li.append(lib.children(item.id).length ? level(item.id) : el("p", { class: "tree-empty", text: "Leeg" }));
        }
      } else {
        row.append(
          el("span", { class: "tree-icon", "aria-hidden": "true", text: "📄" }),
          el("a", { class: "tree-link", href: `#/lijst/${item.id}`, "aria-current": here && "page", text: name }),
        );
      }
      if (ordering) {
        row.append(
          el(
            "span",
            { class: "order-buttons" },
            el("button", {
              type: "button",
              "data-step": "-1",
              "aria-label": `${name} omhoog`,
              disabled: i === 0,
              onclick: () => onMove(parent, itemKey(item), -1),
              text: "↑",
            }),
            el("button", {
              type: "button",
              "data-step": "1",
              "aria-label": `${name} omlaag`,
              disabled: i === items.length - 1,
              onclick: () => onMove(parent, itemKey(item), 1),
              text: "↓",
            }),
          ),
        );
      }
      ul.append(li);
    });
    return ul;
  };

  if (lib.children(ROOT).length === 0) {
    root.replaceChildren(el("p", { class: "tree-empty", text: "Nog geen koppen. Maak er een met + Nieuwe kop." }));
  } else {
    root.replaceChildren(level(ROOT));
  }
}

// focusMoveButton puts focus back on an up/down button after a re-render,
// or on the other one when the item reached the top or bottom.
export function focusMoveButton(root, key, step) {
  const row = root.querySelector(`.tree-item[data-key="${key}"] > .tree-row`);
  const same = row?.querySelector(`[data-step="${step}"]`);
  const other = row?.querySelector(`[data-step="${-step}"]`);
  (same && !same.disabled ? same : other)?.focus();
}

// focusToggle puts focus back on a heading's fold button after a re-render.
export function focusToggle(root, id) {
  root.querySelector(`.tree-item[data-key="heading:${id}"] > .tree-row .tree-toggle`)?.focus();
}
