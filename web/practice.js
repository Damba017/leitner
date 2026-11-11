// A practice session: what is due (first answers change the schedule) or
// free (nothing is stored). Cards come shuffled; a wrong card comes back at
// the end until it is right. Enter checks, Enter again goes on. A session on
// one card (kind "card": the card of a notification) counts like a due one.

import { api } from "./api.js";
import { el, showStatus } from "./ui.js";
import { createCard } from "./card.js";
import { checkAnswer, shuffle, askedDirection } from "./answer.js";
import { languageName } from "./languages.js";
import { canSpeak, speak, spokenText } from "./speech.js";

// practiceHash is the route of a session on a list or heading.
export function practiceHash(kind, id, mode) {
  return `#/oefenen/${kind === "heading" ? "kop" : "lijst"}/${id}${mode === "free" ? "/vrij" : ""}`;
}

const noList = { lang1: "", lang2: "", strictCase: false, strictPunct: false, strictAccents: false };

// sides is what to ask and what to expect for item, in its list's languages.
// Side 1's read-aloud uses the pronunciation when there is one.
function sides(item, list) {
  const one = { text: item.front, lang: list.lang1, speech: item.pronunciation || spokenText(item.front) };
  const two = { text: item.back, lang: list.lang2, speech: spokenText(item.back) };
  return item.direction === "2-1" ? { ask: two, answer: one } : { ask: one, answer: two };
}

function speakButton(label, side) {
  return canSpeak
    ? el("button", {
        type: "button",
        class: "icon secondary",
        "aria-label": `${label} voorlezen`,
        onclick: () => speak(side.speech, side.lang),
        text: "🔊",
      })
    : null;
}

// startPractice runs a session on route ({kind, id, mode}; a card route also
// has its direction) in view; getLib returns the app's current tree.
// loadItems(route) resolves to {items, offline}: a session on offline items
// stores nothing, even if the server comes back. onEnd runs once the stored
// answers are in, so due counts can be fetched again; onUnreachable when
// storing an answer finds the server gone. It returns leave(), which stores a
// pending answer; call it when the page changes.
export function startPractice(view, getLib, route, { loadItems, onEnd, onUnreachable }) {
  const lib = getLib();
  const due = route.mode === "due";
  const single = route.kind === "card";
  let storing = false; // answers go to the server: due and online at the start
  const scope = single ? null : (route.kind === "heading" ? lib.headings : lib.lists).get(route.id);
  // A card's list is known once it is loaded.
  let name = scope?.name ?? "";
  let backHash = single ? "#/" : route.kind === "heading" ? `#/kop/${route.id}` : `#/lijst/${route.id}`;
  let cardList = null;
  const posts = []; // answers being stored
  let left = false;
  let queue = []; // still to ask in this round
  let retry = []; // wrong ones, asked again after the round
  let total = 0; // questions at the start
  let asked = 0; // first attempts shown
  let firstRight = 0;
  let current = null; // {item, first, checked, correct}
  let pending = null; // a wrong first answer, stored when the card is left

  const back = el("a", { class: "back-link", href: backHash, text: single ? "← Overzicht" : `← ${name}` });
  const header = [
    back,
    el("h1", { tabindex: "-1", text: single ? "Kaart uit een melding" : `${due ? "Oefenen" : "Vrij oefenen"}: ${name}` }),
  ];
  const body = el("div", { class: "practice" });
  view.replaceChildren(...header, body);
  view.querySelector("h1").focus({ preventScroll: true });

  // record stores an answer of a due session; failures are shown, the
  // session goes on. Without the server, later answers aren't stored.
  function record(item, correct, keepalive = false) {
    if (!storing) {
      return;
    }
    const p = api("POST", "/api/reviews", { cardId: item.id, direction: item.direction, correct }, undefined, keepalive)
      .catch((err) => {
        if (err.unreachable && storing) {
          storing = false;
          onUnreachable(); // clears the status line, so the message comes after
          showStatus("Geen verbinding: je antwoorden worden niet meer bewaard.", true);
        } else if (!err.unreachable) {
          showStatus(err.message, true);
        }
      });
    posts.push(p);
  }

  function flushPending(keepalive = false) {
    if (pending) {
      record(pending, false, keepalive);
      pending = null;
    }
  }

  const onHide = () => flushPending(true);
  window.addEventListener("pagehide", onHide);

  function end() {
    Promise.allSettled(posts).then(onEnd);
  }

  function message(...children) {
    body.replaceChildren(...children);
    body.querySelector("h2")?.focus({ preventScroll: true });
  }

  async function load() {
    body.replaceChildren(el("p", { text: "Kaarten laden…" }));
    let items;
    try {
      let offline;
      ({ items, offline } = await loadItems(route));
      storing = due && !offline; // a due session loads once: fixed for the session
    } catch (err) {
      if (!left) {
        message(el("p", { class: "error", role: "alert", text: err.message }), backLink());
      }
      return;
    }
    if (left) {
      return;
    }
    if (single) {
      if (!items.length) {
        message(
          el("h2", { tabindex: "-1", text: "Niet gevonden" }),
          el("p", { text: "Deze kaart bestaat niet meer." }),
          el("div", { class: "row actions" }, el("a", { class: "button-link", href: "#/", text: "Naar het overzicht" })),
        );
        return;
      }
      // Unknown when the tree is still loading: then the card goes on without.
      cardList = getLib().lists.get(items[0].listId) ?? null;
      if (cardList) {
        name = cardList.name;
        backHash = `#/lijst/${cardList.id}`;
        back.href = backHash;
        back.textContent = `← ${name}`;
      }
    }
    queue = shuffle(items.map((it) => ({ ...it, direction: askedDirection(it.direction), repeat: false })));
    retry = [];
    total = queue.length;
    asked = 0;
    firstRight = 0;
    if (total === 0) {
      message(
        el("h2", { tabindex: "-1", text: due ? "Niets te doen" : "Geen kaarten" }),
        el("p", { text: due ? "Alles is bij. Je kunt wel vrij oefenen." : "Hier staan nog geen kaarten." }),
        el(
          "div",
          { class: "row actions" },
          due ? el("a", { class: "button-link primary", href: practiceHash(route.kind, route.id, "free"), text: "Vrij oefenen" }) : null,
          backLink(),
        ),
      );
      return;
    }
    showNext();
  }

  const backLink = () =>
    el("a", { class: "button-link", href: backHash, text: name ? `Terug naar „${name}”` : "Naar het overzicht" });

  function showNext() {
    if (queue.length === 0 && retry.length > 0) {
      queue = shuffle(retry);
      retry = [];
    }
    if (queue.length === 0) {
      finish();
      return;
    }
    const item = queue.shift();
    current = { item, first: !item.repeat, checked: false, correct: false };
    if (current.first) {
      asked++;
    }
    render();
  }

  // The elements of the card on screen.
  let ui = null;

  function render() {
    const { item } = current;
    const list = getLib().lists.get(item.listId) ?? noList;
    const { ask, answer } = sides(item, list);
    const card = createCard({
      front: { text: ask.text, lang: ask.lang, label: languageName(ask.lang) },
      back: { text: answer.text, lang: answer.lang, label: languageName(answer.lang) },
      interactive: false,
    });
    const input = el("input", {
      id: "answer",
      type: "text",
      lang: answer.lang || null,
      autocomplete: "off",
      autocorrect: "off",
      autocapitalize: "none",
      spellcheck: "false",
      enterkeyhint: "done",
    });
    const submit = el("button", { type: "submit", text: "Controleren" });
    const feedback = el("p", { class: "feedback", role: "status" });
    const note = el("p", { class: "card-note", hidden: true }, el("span", { class: "visually-hidden", text: "Notitie: " }), item.note);
    const answerSpeak = speakButton("Antwoord", answer);
    if (answerSpeak) {
      answerSpeak.hidden = true;
    }
    const override = el("button", { type: "button", class: "secondary", hidden: true, onclick: acceptAnyway, text: "Toch goed" });
    const form = el(
      "form",
      { class: "practice-form", onsubmit: (e) => (e.preventDefault(), act()) },
      el("label", { for: "answer", text: `Antwoord (${languageName(answer.lang) || "kant " + (item.direction === "1-2" ? 2 : 1)})` }),
      el("div", { class: "row" }, input, submit),
    );
    input.addEventListener("keydown", (e) => {
      // A held-down Enter shouldn't check and skip the card at once.
      if (e.key === "Enter" && !e.isComposing && !e.repeat) {
        e.preventDefault();
        act();
      }
    });
    ui = { card, input, submit, feedback, note, answerSpeak, override, list, answer };
    body.replaceChildren(
      el("p", { class: "practice-progress muted", text: progressText() }),
      due && !storing ? el("p", { class: "hint", text: "Offline: je antwoorden worden niet bewaard." }) : null,
      card.element,
      el("div", { class: "row practice-speak" }, speakButton("Vraag", ask), answerSpeak),
      form,
      feedback,
      note,
      el(
        "div",
        { class: "row actions" },
        override,
        el("a", { class: "button-link", href: backHash, text: "Stoppen" }),
      ),
    );
    input.focus({ preventScroll: true });
  }

  function progressText() {
    if (!current.first) {
      return `Herhalen · nog ${queue.length + retry.length + 1}`;
    }
    const again = retry.length ? ` · ${retry.length} om te herhalen` : "";
    return `Kaart ${asked} van ${total}${again}`;
  }

  function act() {
    if (current.checked) {
      next();
    } else {
      check();
    }
  }

  function check() {
    const { list, answer, input } = ui;
    const result = checkAnswer(input.value, answer.text, {
      strictCase: list.strictCase,
      strictPunct: list.strictPunct,
      strictAccents: list.strictAccents,
      lang: answer.lang,
    });
    current.checked = true;
    current.correct = result.correct;
    input.readOnly = true;
    ui.card.flip(true);
    ui.submit.textContent = "Volgende";
    ui.note.hidden = !current.item.note;
    if (ui.answerSpeak) {
      ui.answerSpeak.hidden = false;
    }
    ui.override.hidden = result.correct;
    let text;
    if (result.empty) {
      text = `Het antwoord is: ${answer.text}`;
    } else if (result.correct) {
      text = result.accentWarning ? `Goed, maar let op de accenten: ${answer.text}` : "Goed!";
    } else {
      text = result.accentWarning ? `Fout: alleen de accenten kloppen niet. Goed is: ${answer.text}` : `Fout. Goed is: ${answer.text}`;
    }
    showVerdict(result.correct, text);
    if (due && current.first) {
      if (result.correct) {
        record(current.item, true);
      } else {
        pending = current.item;
      }
    }
  }

  function showVerdict(correct, text) {
    ui.feedback.className = `feedback ${correct ? "is-right" : "is-wrong"}`;
    ui.feedback.textContent = text;
  }

  // acceptAnyway turns a wrong verdict into a right one ("Toch goed").
  function acceptAnyway() {
    current.correct = true;
    ui.override.hidden = true;
    showVerdict(true, `Toch goed gerekend. Goed is: ${ui.answer.text}`);
    if (pending === current.item) {
      pending = null;
      record(current.item, true);
    }
    ui.input.focus({ preventScroll: true });
  }

  function next() {
    flushPending();
    if (current.first && current.correct) {
      firstRight++;
    }
    if (!current.correct) {
      retry.push({ ...current.item, repeat: true });
    }
    showNext();
  }

  function finish() {
    ui = null;
    if (single) {
      message(
        el("h2", { tabindex: "-1", text: "Klaar!" }),
        el("p", { text: firstRight ? "In één keer goed." : "Goed na een herhaling." }),
        storing ? el("p", { class: "hint", text: "Je antwoord is bewaard. De kaart komt terug zodra hij weer te doen is." }) : null,
        storing ? null : el("p", { class: "hint", text: "Je antwoord is niet bewaard: je was offline." }),
        el(
          "div",
          { class: "row actions" },
          cardList
            ? el("a", { class: "button-link primary", href: practiceHash("list", cardList.id, "due"), text: `Oefen „${name}”` })
            : null,
          backLink(),
        ),
      );
      end();
      return;
    }
    message(
      el("h2", { tabindex: "-1", text: "Klaar!" }),
      el("p", { text: `${firstRight} van ${total} in één keer goed.` }),
      due && storing ? el("p", { class: "hint", text: "Je antwoorden zijn bewaard. Elke kaart komt terug zodra hij weer te doen is." }) : null,
      due && !storing ? el("p", { class: "hint", text: "Je antwoorden zijn niet bewaard: je was offline." }) : null,
      el(
        "div",
        { class: "row actions" },
        due
          ? el("a", { class: "button-link primary", href: practiceHash(route.kind, route.id, "free"), text: "Vrij oefenen" })
          : el("button", { type: "button", onclick: load, text: "Nog eens" }),
        backLink(),
      ),
    );
    end();
  }

  load();

  return function leave() {
    if (left) {
      return;
    }
    left = true;
    window.removeEventListener("pagehide", onHide);
    flushPending();
    end();
  };
}
