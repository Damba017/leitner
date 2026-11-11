// Flash card with a flip animation, used by practice (practice.js). An
// interactive card is a button: click, Enter or Space flips it; practice
// uses a plain one and flips it when the answer is checked. Only the face
// that's showing is visible to screen readers (see app.css).

import { el } from "./ui.js";

// createCard returns {element, flip, flipped}. front and back are
// {text, lang, label, action}: lang sets the language for screen readers
// and speech, label is the small line under the text (e.g. "Spaans"),
// action an optional button in the face's corner (not on an interactive
// card: no button inside a button).
export function createCard({ front, back, interactive = true }) {
  const face = (side, cls) =>
    el(
      "span",
      { class: `flipcard-face ${cls}` },
      side.label ? el("span", { class: "flipcard-side", text: side.label }) : null,
      el("span", { class: side.text.length > 40 ? "flipcard-text is-long" : "flipcard-text", lang: side.lang || null, text: side.text }),
      interactive ? null : side.action,
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
