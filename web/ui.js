// Small DOM helpers shared by the app's modules.

const skip = (c) => c !== null && c !== undefined && c !== false;

// el creates an element. props sets attributes, except "text" (textContent)
// and on* functions (event listeners); null, undefined and false are left
// out. children are nodes or strings (as text, never HTML).
export function el(tag, props = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (!skip(value)) {
      continue;
    }
    if (key === "text") {
      node.textContent = value;
    } else if (key.startsWith("on") && typeof value === "function") {
      node.addEventListener(key.slice(2), value);
    } else {
      node.setAttribute(key, value === true ? "" : String(value));
    }
  }
  node.append(...children.flat().filter(skip));
  return node;
}

// fill replaces node's children like el() adds them: null, undefined and
// false are left out (replaceChildren would show them as text).
export function fill(node, ...children) {
  node.replaceChildren(...children.flat().filter(skip));
}

const SVG = "http://www.w3.org/2000/svg";

// The icons' paths on a 24×24 grid; app.css draws them as round strokes in
// the text colour.
const ICONS = {
  chevron: ["M9.5 6l6 6-6 6"],
  folder: ["M3.5 7.5a2 2 0 0 1 2-2h3.6l2 2h7.4a2 2 0 0 1 2 2v7a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2z"],
  cards: ["M8.5 4.5h9a2 2 0 0 1 2 2v9", "M6.5 8h8a2 2 0 0 1 2 2v7.5a2 2 0 0 1-2 2h-8a2 2 0 0 1-2-2V10a2 2 0 0 1 2-2z"],
  speaker: ["M11 5.5 6.5 9H4v6h2.5l4.5 3.5z", "M15 9.5a3.5 3.5 0 0 1 0 5", "M17.8 7a7 7 0 0 1 0 10"],
  pencil: ["M4.5 19.5h4l10-10-4-4-10 10z", "M13 7l4 4"],
  trash: ["M4.5 7h15", "M9.5 7V4.5h5V7", "M6.5 7l1 12.5h9l1-12.5"],
  plus: ["M12 5v14", "M5 12h14"],
  check: ["M5 12.5l4.5 4.5L19 7.5"],
  cross: ["M6.5 6.5l11 11", "M17.5 6.5l-11 11"],
  grip: ["M9 6h.01", "M15 6h.01", "M9 12h.01", "M15 12h.01", "M9 18h.01", "M15 18h.01"],
};

// icon returns a decorative SVG icon, hidden from screen readers.
export function icon(name) {
  const svg = document.createElementNS(SVG, "svg");
  svg.setAttribute("class", `icon icon-${name}`);
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("focusable", "false");
  for (const d of ICONS[name]) {
    const path = document.createElementNS(SVG, "path");
    path.setAttribute("d", d);
    svg.append(path);
  }
  return svg;
}

// focusKey names a focused element so the same control can be found again
// after its part of the page was drawn anew: a selector for its data-focus,
// or for a link's href. null when it can't be found again.
export function focusKey(node) {
  if (!(node instanceof HTMLElement)) {
    return null;
  }
  if (node.dataset.focus) {
    return `[data-focus="${CSS.escape(node.dataset.focus)}"]`;
  }
  const href = node.tagName === "A" ? node.getAttribute("href") : null;
  return href ? `a[href="${CSS.escape(href)}"]` : null;
}

// refocus moves focus to the element of key inside root, but only when
// focus was lost (it fell back to the body).
export function refocus(root, key) {
  if (key && (!document.activeElement || document.activeElement === document.body)) {
    root.querySelector(key)?.focus({ preventScroll: true });
  }
}

// showStatus shows a message above the page; error ones in red. An empty
// message hides it.
export function showStatus(message, isError = false) {
  const status = document.getElementById("status");
  status.textContent = message;
  status.classList.toggle("is-error", isError);
  status.setAttribute("role", isError ? "alert" : "status");
  status.hidden = !message;
}
