// Forms for headings and lists, and the delete confirmation, as <dialog>s.
// No confirm(): it didn't always show in Safari (nestje).

import { el } from "./ui.js";
import { api } from "./api.js";
import { ROOT } from "./state.js";
import { languages, isListed, languageName } from "./languages.js";

const OTHER = "other";

// setup wires a dialog's form once. While it is open, ctx.submit does the
// work: a thrown error stays in the dialog; on success the dialog closes
// first (so focus has returned) and then ctx.done gets the result.
function setup(id) {
  const dialog = document.getElementById(id);
  const form = dialog.querySelector("form");
  const error = dialog.querySelector(".error");
  const submit = form.querySelector('[type="submit"]');
  const ctx = { dialog, form, submit: null, done: null };

  const showError = (message) => {
    error.textContent = message;
    error.hidden = !message;
  };
  ctx.showError = showError;

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    if (!ctx.submit || submit.disabled) {
      return;
    }
    showError("");
    submit.disabled = true;
    try {
      const result = await ctx.submit();
      if (ctx.stay) {
        // Adding several cards in a row: stay open, the caller clears the form.
        ctx.done?.(result);
        ctx.onStay?.(result);
      } else {
        dialog.close();
        ctx.done?.(result);
      }
    } catch (err) {
      showError(err.message);
    } finally {
      submit.disabled = false;
    }
  });
  dialog.querySelectorAll("[data-close]").forEach((b) => b.addEventListener("click", () => dialog.close()));

  ctx.open = (submitFn, doneFn, { stay = false, onStay = null } = {}) => {
    ctx.submit = submitFn;
    ctx.done = doneFn;
    ctx.stay = stay;
    ctx.onStay = onStay;
    showError("");
    submit.disabled = false;
    dialog.showModal();
  };
  return ctx;
}

let headingDialog, listDialog, deleteDialog, cardDialog, pasteDialog, csvDialog;

export function setupDialogs() {
  headingDialog = setup("heading-dialog");
  listDialog = setup("list-dialog");
  deleteDialog = setup("delete-dialog");
  cardDialog = setup("card-dialog");
  pasteDialog = setup("paste-dialog");
  csvDialog = setup("csv-dialog");
  for (const select of listDialog.form.querySelectorAll("select[data-other]")) {
    select.replaceChildren(
      el("option", { value: "", text: "Kies een taal" }),
      ...languages.map(([code, name]) => el("option", { value: code, text: name })),
      el("option", { value: OTHER, text: "Anders…" }),
    );
    select.addEventListener("change", () => syncOther(select));
  }
}

// fillHeadings fills select with the headings of lib, indented by depth.
function fillHeadings(select, lib, { skipId = null, topLevel = false, selected = null }) {
  const options = lib.headingOptions(skipId).map((h) =>
    el("option", { value: h.id, text: `${"\u00a0\u00a0\u00a0".repeat(h.depth)}${h.name}` }),
  );
  if (topLevel) {
    options.unshift(el("option", { value: ROOT, text: "— Bovenste niveau —" }));
  }
  select.replaceChildren(...options);
  select.value = selected === null ? (topLevel ? ROOT : "") : String(selected);
}

// openHeadingDialog creates a heading under parentId, or edits heading.
// onSaved gets the saved heading.
export function openHeadingDialog(lib, { heading = null, parentId = null }, onSaved) {
  const { form } = headingDialog;
  document.getElementById("heading-dialog-title").textContent = heading ? "Kop bewerken" : parentId ? "Nieuwe subkop" : "Nieuwe kop";
  form.elements.name.value = heading?.name ?? "";
  fillHeadings(form.elements.parent, lib, {
    skipId: heading?.id ?? null,
    topLevel: true,
    selected: heading ? heading.parentId : parentId,
  });
  headingDialog.open(async () => {
    const parent = form.elements.parent.value;
    const body = { name: form.elements.name.value, parentId: parent === ROOT ? null : Number(parent) };
    return heading ? api("PUT", `/api/headings/${heading.id}`, body) : api("POST", "/api/headings", body);
  }, onSaved);
}

// syncOther shows the free code field when "Anders…" is chosen.
function syncOther(select) {
  const other = document.getElementById(select.dataset.other);
  other.hidden = select.value !== OTHER;
  other.required = !other.hidden;
}

function setLang(select, code) {
  const other = document.getElementById(select.dataset.other);
  if (!code || isListed(code)) {
    select.value = code ?? "";
    other.value = "";
  } else {
    select.value = OTHER;
    other.value = code;
  }
  syncOther(select);
}

function readLang(select) {
  if (select.value !== OTHER) {
    return select.value;
  }
  return document.getElementById(select.dataset.other).value.trim();
}

// openListDialog creates a list in headingId, or edits list. A new list
// takes its languages and checks from the last list in that heading, which
// usually is the same subject.
export function openListDialog(lib, { list = null, headingId = null }, onSaved) {
  const { form } = listDialog;
  const f = form.elements;
  document.getElementById("list-dialog-title").textContent = list ? "Lijst bewerken" : "Nieuwe lijst";
  let model = list;
  if (!model) {
    const lastList = lib
      .children(headingId)
      .filter((i) => i.kind === "list")
      .map((i) => lib.lists.get(i.id))
      .pop();
    model = {
      name: "",
      headingId,
      lang1: lastList?.lang1 ?? "",
      lang2: lastList?.lang2 ?? "nl",
      direction: lastList?.direction ?? "1-2",
      strictCase: lastList?.strictCase ?? false,
      strictPunct: lastList?.strictPunct ?? false,
      strictAccents: lastList?.strictAccents ?? false,
    };
  }
  f.name.value = model.name;
  fillHeadings(f.heading, lib, { selected: model.headingId });
  setLang(f.lang1, model.lang1);
  setLang(f.lang2, model.lang2);
  f.direction.value = model.direction;
  f.strictCase.checked = model.strictCase;
  f.strictPunct.checked = model.strictPunct;
  f.strictAccents.checked = model.strictAccents;

  listDialog.open(async () => {
    const lang1 = readLang(f.lang1);
    const lang2 = readLang(f.lang2);
    if (!lang1 || !lang2) {
      throw new Error("Kies voor beide kanten een taal.");
    }
    const body = {
      headingId: Number(f.heading.value),
      name: f.name.value,
      lang1,
      lang2,
      direction: f.direction.value,
      strictCase: f.strictCase.checked,
      strictPunct: f.strictPunct.checked,
      strictAccents: f.strictAccents.checked,
    };
    return list ? api("PUT", `/api/lists/${list.id}`, body) : api("POST", "/api/lists", body);
  }, onSaved);
}

const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;

// openDeleteDialog asks before deleting a heading (with what's in it) or a list.
export function openDeleteDialog(lib, { kind, id }, onDeleted) {
  const isHeading = kind === "heading";
  const item = (isHeading ? lib.headings : lib.lists).get(id);
  document.getElementById("delete-dialog-title").textContent = `${isHeading ? "Kop" : "Lijst"} „${item.name}” verwijderen?`;
  let note = "Dit kan niet ongedaan worden gemaakt.";
  if (isHeading) {
    const c = lib.contents(id);
    const parts = [];
    if (c.headings) {
      parts.push(plural(c.headings, "subkop", "subkoppen"));
    }
    if (c.lists) {
      parts.push(plural(c.lists, "lijst", "lijsten"));
    }
    if (parts.length) {
      note = `Hiermee verdwijnen ook ${parts.join(" en ")}, met alles erin. ${note}`;
    }
  }
  if (!isHeading) {
    note = `Hiermee verdwijnen ook alle kaarten in deze lijst. ${note}`;
  }
  document.getElementById("delete-note").textContent = note;
  deleteDialog.open(() => api("DELETE", `/api/${isHeading ? "headings" : "lists"}/${id}`), onDeleted);
}

// openCardDialog adds cards to list one after another (the dialog stays
// open, "Klaar" closes it), or edits card. onSaved runs after every save.
export function openCardDialog(list, { card = null }, onSaved) {
  const { form } = cardDialog;
  const f = form.elements;
  const added = document.getElementById("card-added");
  document.getElementById("card-dialog-title").textContent = card ? "Kaart bewerken" : "Nieuwe kaart";
  document.getElementById("card-front-label").textContent = `Kant 1 (${languageName(list.lang1)})`;
  document.getElementById("card-back-label").textContent = `Kant 2 (${languageName(list.lang2)})`;
  document.getElementById("card-submit").textContent = card ? "Opslaan" : "Toevoegen";
  document.getElementById("card-close").textContent = card ? "Annuleren" : "Klaar";
  added.hidden = true;
  f.front.value = card?.front ?? "";
  f.back.value = card?.back ?? "";
  f.note.value = card?.note ?? "";
  f.pronunciation.value = card?.pronunciation ?? "";

  const submit = async () => {
    const body = {
      front: f.front.value,
      back: f.back.value,
      note: f.note.value,
      pronunciation: f.pronunciation.value,
    };
    return card ? api("PUT", `/api/cards/${card.id}`, body) : api("POST", `/api/lists/${list.id}/cards`, body);
  };
  const onStay = (saved) => {
    form.reset();
    added.textContent = `Toegevoegd: ${saved.front} — ${saved.back}`;
    added.hidden = false;
    f.front.focus();
  };
  cardDialog.open(submit, onSaved, card ? {} : { stay: true, onStay });
}

// openPasteDialog adds a pasted list; onSaved gets {added}.
export function openPasteDialog(list, onSaved) {
  const { form } = pasteDialog;
  form.reset();
  pasteDialog.open(
    () => api("POST", `/api/lists/${list.id}/paste`, { text: form.elements.text.value, swap: form.elements.swap.checked }),
    onSaved,
  );
}

// openCsvDialog imports a CSV file; onSaved gets {added}.
export function openCsvDialog(list, onSaved) {
  const { form } = csvDialog;
  form.reset();
  csvDialog.open(async () => {
    const file = form.elements.file.files[0];
    if (!file) {
      throw new Error("Kies eerst een bestand.");
    }
    return api("POST", `/api/lists/${list.id}/import`, await file.text(), "text/csv");
  }, onSaved);
}

// openDeleteCardDialog asks before deleting one card.
export function openDeleteCardDialog(card, onDeleted) {
  document.getElementById("delete-dialog-title").textContent = `Kaart „${card.front}” verwijderen?`;
  document.getElementById("delete-note").textContent = "Dit kan niet ongedaan worden gemaakt.";
  deleteDialog.open(() => api("DELETE", `/api/cards/${card.id}`), onDeleted);
}
