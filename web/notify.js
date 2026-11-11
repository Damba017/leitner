// The Meldingen tab (#/meldingen): notifications on this device, the user's
// devices, when notifications come (interval, quiet hours; the same on every
// device) and from which headings and lists. Needs the server: offline it
// only says so.

import { api } from "./api.js";
import { el, icon } from "./ui.js";
import { ROOT } from "./state.js";
import { isOffline } from "./offline.js";
import * as push from "./push.js";

const INTERVALS = [
  [0, "Uit"],
  [30, "Elk half uur"],
  [60, "Elk uur"],
  [120, "Elke 2 uur"],
  [180, "Elke 3 uur"],
  [240, "Elke 4 uur"],
  [1440, "Eén keer per dag"],
];

function show(node, text) {
  node.textContent = text;
  node.hidden = !text;
}

function formatDate(sec) {
  return new Date(sec * 1000).toLocaleDateString("nl-NL", {
    timeZone: "Europe/Amsterdam",
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
  });
}

const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;

// startNotify draws the tab in view; getLib returns the app's current tree.
// The tab draws itself, so the app leaves it alone until the route changes;
// it returns leave() for that moment.
export function startNotify(view, getLib) {
  let left = false;
  let reg = null; // ServiceWorkerRegistration; null where push can't work
  let current = null; // this browser's PushSubscription
  let key = null; // the server's public key, for subscribing
  let data = null; // GET /api/notify

  const title = el("h1", { tabindex: "-1", text: "Meldingen" });
  const intro = el("p", {
    text: "Leitner stuurt je op vaste tijden één kaart: de kaart die het langst te doen is. Tik op de melding en typ het antwoord; dat telt mee voor je schema.",
  });

  // This device and the list of devices.
  const status = el("p", { class: "device-status" });
  const toggle = el("button", { type: "button", hidden: true, onclick: () => run(mine() ? turnOff : turnOn) });
  const test = el("button", { type: "button", class: "secondary", hidden: true, onclick: () => run(sendTest), text: "Testmelding" });
  const hint = el("p", { class: "hint", hidden: true });
  const error = el("p", { class: "error", role: "alert", hidden: true });
  const notice = el("p", { class: "notice", role: "status", hidden: true });
  const devices = el("ul", { class: "devices" });
  const deviceButtons = () => [toggle, test, ...devices.querySelectorAll("button")];

  // When.
  const interval = el(
    "select",
    { id: "notify-interval", name: "interval" },
    INTERVALS.map(([v, label]) => el("option", { value: v, text: label })),
  );
  const quietOn = el("input", { type: "checkbox", name: "quietOn" });
  const quietFrom = el("input", { type: "time", id: "quiet-from", name: "quietFrom", required: true, step: 60 });
  const quietUntil = el("input", { type: "time", id: "quiet-until", name: "quietUntil", required: true, step: 60 });
  const formError = el("p", { class: "error", role: "alert", hidden: true });
  const formNotice = el("p", { class: "notice", role: "status", hidden: true });
  const save = el("button", { type: "submit", text: "Opslaan" });
  const form = el(
    "form",
    { class: "stack", novalidate: true, onsubmit: (e) => (e.preventDefault(), saveSettings()) },
    el("label", { for: "notify-interval", text: "Hoe vaak" }),
    interval,
    el("label", { class: "choice" }, quietOn, "Stille uren (geen meldingen)"),
    el(
      "div",
      { class: "row quiet-times" },
      el("label", { for: "quiet-from", text: "Van" }),
      quietFrom,
      el("label", { for: "quiet-until", text: "tot" }),
      quietUntil,
    ),
    el("p", {
      class: "hint",
      text: "De meldingen van een dag beginnen om de eindtijd van de stille uren, daarna steeds na het interval: bijvoorbeeld 08:00, 09:00, 10:00. Tijden in Nederlandse tijd.",
    }),
    formError,
    formNotice,
    save,
  );

  // Which headings and lists.
  const tree = el("div", { class: "notify-tree" });
  const treeError = el("p", { class: "error", role: "alert", hidden: true });

  const body = el("div", {}, el("p", { text: "Laden…" }));
  view.replaceChildren(el("a", { class: "back-link", href: "#/", text: "← Overzicht" }), title, intro, body);
  title.focus({ preventScroll: true });

  function mine() {
    return current && data ? data.devices.find((d) => d.endpoint === current.endpoint) : undefined;
  }

  function renderDevice() {
    const on = Boolean(mine());
    const isBlocked = reg && !on && push.blocked();
    show(status, reg ? (on ? "Meldingen staan aan op dit apparaat." : "Meldingen staan uit op dit apparaat.") : "");
    toggle.hidden = !reg || isBlocked;
    toggle.textContent = on ? "Meldingen uit" : "Meldingen aan";
    toggle.className = on ? "secondary" : "";
    test.hidden = !data.devices.length;
    let h = push.unsupported();
    if (!h && isBlocked) {
      h = "Meldingen zijn geblokkeerd voor Leitner. Zet ze aan in de instellingen van je browser of telefoon.";
    }
    if (!h && !data.canSend) {
      h = "Versturen kan pas als de server met -url https://… draait.";
    }
    show(hint, h);

    const me = mine();
    devices.replaceChildren(
      ...(data.devices.length
        ? data.devices.map((d) =>
            el(
              "li",
              {},
              el(
                "span",
                {},
                el("strong", { text: d.label || "Onbekend apparaat" }),
                el("span", { class: "muted", text: ` · sinds ${formatDate(d.created)}${d === me ? " (dit apparaat)" : ""}` }),
              ),
              el("button", {
                type: "button",
                class: "danger small",
                "aria-label": `${d.label || "Onbekend apparaat"} verwijderen`,
                onclick: () => run(d === me ? turnOff : () => api("DELETE", `/api/push/subscriptions/${d.id}`)),
                text: "Verwijderen",
              }),
            ),
          )
        : [el("li", { class: "muted", text: "Nog op geen enkel apparaat aan." })]),
    );
  }

  function fillForm() {
    const s = data.settings;
    interval.value = String(s.interval);
    quietOn.checked = s.quietOn;
    quietFrom.value = s.quietFrom;
    quietUntil.value = s.quietUntil;
  }

  // renderTree draws the headings and lists with a switch each. Under a
  // heading that is off, the switches can't be used: everything is off.
  function renderTree() {
    const lib = getLib();
    const walk = (parent, silenced) => {
      const items = lib.children(parent);
      return items.length
        ? el(
            "ul",
            { class: "notify-list" },
            items.map((item) => {
              const isHeading = item.kind === "heading";
              const node = (isHeading ? lib.headings : lib.lists).get(item.id);
              const box = el("input", {
                type: "checkbox",
                "data-key": `${item.kind}:${item.id}`,
                checked: node.notify && !silenced,
                disabled: silenced,
                onchange: (e) => setSwitch(item, e.target),
              });
              return el(
                "li",
                {},
                el(
                  "label",
                  { class: "choice" },
                  box,
                  icon(isHeading ? "folder" : "cards"),
                  el("span", { text: lib.name(item) }),
                  silenced ? el("span", { class: "muted", text: " (uit via kop)" }) : null,
                ),
                isHeading ? walk(item.id, silenced || !node.notify) : null,
              );
            }),
          )
        : null;
    };
    tree.replaceChildren(walk(ROOT, false) ?? el("p", { class: "muted", text: "Je hebt nog geen koppen." }));
  }

  async function setSwitch(item, box) {
    const on = box.checked;
    const lib = getLib();
    const node = (item.kind === "heading" ? lib.headings : lib.lists).get(item.id);
    show(treeError, "");
    box.disabled = true;
    try {
      await api("PUT", `/api/${item.kind === "heading" ? "headings" : "lists"}/${item.id}/notify`, { on });
      if (node) {
        node.notify = on;
      }
    } catch (err) {
      show(treeError, err.message);
    }
    if (!left) {
      renderTree();
      tree.querySelector(`input[data-key="${item.kind}:${item.id}"]`)?.focus();
    }
  }

  async function load() {
    data = await api("GET", "/api/notify");
    key = push.fromBase64url(data.publicKey);
    if (reg) {
      current = await push.currentSubscription(reg, key);
    }
  }

  async function turnOn() {
    // Straight from the click: Safari only asks for permission then.
    current = await push.subscribe(reg, key);
    await api("POST", "/api/push/subscriptions", push.subscriptionBody(current));
  }

  async function turnOff() {
    const d = mine();
    await current.unsubscribe();
    current = null;
    if (d) {
      await api("DELETE", `/api/push/subscriptions/${d.id}`);
    }
  }

  async function sendTest() {
    const r = await api("POST", "/api/push/test");
    if (!r.sent && !r.removed && !r.failed) {
      show(notice, "Er staan nog op geen enkel apparaat meldingen aan.");
      return;
    }
    let msg = `Verstuurd naar ${plural(r.sent, "apparaat", "apparaten")}.`;
    if (r.removed) {
      msg += ` ${plural(r.removed, "verlopen apparaat", "verlopen apparaten")} verwijderd.`;
    }
    if (r.failed) {
      msg += ` Bij ${plural(r.failed, "apparaat", "apparaten")} lukte het niet.`;
    }
    show(notice, msg);
  }

  // run calls fn right away (still inside the click), then reloads the
  // devices. The focused button may disappear, so focus goes to the status.
  async function run(fn) {
    show(error, "");
    show(notice, "");
    for (const b of deviceButtons()) {
      b.disabled = true;
    }
    try {
      await fn();
    } catch (err) {
      show(error, err.message);
    }
    try {
      await load();
    } catch (err) {
      show(error, err.message);
    }
    if (left) {
      return;
    }
    renderDevice();
    for (const b of deviceButtons()) {
      b.disabled = false;
    }
    if (!document.activeElement || document.activeElement === document.body) {
      toggle.hidden ? title.focus({ preventScroll: true }) : toggle.focus({ preventScroll: true });
    }
  }

  async function saveSettings() {
    show(formError, "");
    show(formNotice, "");
    save.disabled = true;
    try {
      await api("PUT", "/api/notify/settings", {
        interval: Number(interval.value),
        quietOn: quietOn.checked,
        quietFrom: quietFrom.value,
        quietUntil: quietUntil.value,
      });
      show(formNotice, Number(interval.value) ? "Opgeslagen. De volgende melding komt op het eerstvolgende tijdstip." : "Opgeslagen. Er komen geen meldingen meer.");
    } catch (err) {
      show(formError, err.message);
    }
    save.disabled = false;
  }

  async function init() {
    if (isOffline()) {
      body.replaceChildren(el("p", { text: "Meldingen instellen kan alleen online." }));
      return;
    }
    if (!push.unsupported()) {
      reg = await push.registration();
      if (!reg) {
        show(error, "De service worker startte niet, dus meldingen werken hier niet.");
      }
    }
    try {
      await load();
    } catch (err) {
      if (!left) {
        body.replaceChildren(el("p", { class: "error", role: "alert", text: err.message }));
      }
      return;
    }
    if (left) {
      return;
    }
    renderDevice();
    fillForm();
    renderTree();
    body.replaceChildren(
      el(
        "section",
        { class: "panel" },
        el("h2", { text: "Dit apparaat" }),
        status,
        hint,
        el("div", { class: "row actions" }, toggle, test),
        error,
        notice,
        el("h3", { text: "Je apparaten" }),
        devices,
      ),
      el("section", { class: "panel" }, el("h2", { text: "Wanneer" }), form),
      el(
        "section",
        { class: "panel" },
        el("h2", { text: "Van welke koppen en lijsten" }),
        el("p", { class: "hint", text: "Zet een kop of lijst uit om er geen meldingen van te krijgen. Een kop die uit staat, zet alles erin uit." }),
        treeError,
        tree,
      ),
    );
  }

  init();

  return function leave() {
    left = true;
  };
}
