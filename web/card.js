// Flash card with a flip animation, for the card pages (phase 4) and
// practice (phase 5). The card is a button: click, Enter or Space flips it.
// Only the face that's showing is visible to screen readers (see app.css).

import { el } from "./ui.js";

// createCard returns {element, flip, flipped}. front and back are
// {text, lang, label}: lang sets the language for screen readers and
// speech, label is the small line under the text (e.g. "Spaans").
export function createCard({ front, back, interactive = true }) {
  const face = (side, cls) =>
    el(
      "span",
      { class: `flipcard-face ${cls}` },
      el("span", { class: "flipcard-text", lang: side.lang || null, text: side.text }),
      side.label ? el("span", { class: "flipcard-side", text: side.label }) : null,
    );

  const element = el(
    interactive ? "button" : "div",
    { class: "flipcard", type: interactive ? "button" : null },
    el("span", { class: "flipcard-inner" }, face(front, "flipcard-front"), face(back, "flipcard-back")),
    interactive ? el("span", { class: "visually-hidden", text: ", omdraaien" }) : null,
  );

  // flip turns the card over, or to a given side: true = back.
  const flip = (toBack = !flipped()) => {
    element.classList.toggle("is-flipped", toBack);
  };
  const flipped = () => element.classList.contains("is-flipped");

  if (interactive) {
    element.addEventListener("click", () => flip());
  }
  return { element, flip, flipped };
}
