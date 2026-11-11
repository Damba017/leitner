// Tests for the push and notificationclick handlers of web/sw.js. Run by
// js_test.go as `jsc -m testdata/sw_test.js`; prints each failure and throws
// if any. The worker runs against a fake `self` that records what it does.

const failures = [];
function check(name, ok, detail = "") {
  if (!ok) {
    failures.push(`${name}${detail ? ": " + detail : ""}`);
  }
}

const listeners = {};
const shown = []; // [title, options] per notification
let refuseOptions = false; // showNotification rejects anything with a tag
let windows = []; // fake WindowClients
const opened = [];

// jsc has no URL: just enough of one for absolute URLs and paths with a base.
class FakeURL {
  constructor(raw, base) {
    const abs = /^([a-z][a-z0-9+.-]*):\/\/([^/?#]*)(.*)$/i.exec(raw);
    if (abs) {
      if (/[[\]]/.test(abs[2]) && !/^\[[0-9a-f:]+\](:\d+)?$/i.test(abs[2])) {
        throw new TypeError("Invalid URL");
      }
      this.origin = `${abs[1].toLowerCase()}://${abs[2].toLowerCase()}`;
      this.rest = abs[3] || "/";
    } else if (base !== undefined && raw.startsWith("/")) {
      this.origin = new FakeURL(base).origin;
      this.rest = raw;
    } else {
      throw new TypeError("Invalid URL");
    }
    this.href = this.origin + this.rest;
  }
}
globalThis.URL ??= FakeURL;

globalThis.VERSION = "test";
globalThis.APP_FILES = ["/"];
globalThis.self = {
  location: { origin: "https://leitner.example.com:8443" },
  addEventListener(type, fn) {
    listeners[type] = fn;
  },
  registration: {
    async showNotification(title, options) {
      if (refuseOptions && options.tag) {
        throw new TypeError("refused");
      }
      shown.push([title, options]);
    },
  },
  clients: {
    async matchAll() {
      return windows;
    },
    async openWindow(url) {
      opened.push(url);
    },
    claim() {},
  },
  skipWaiting() {},
};

load("web/sw.js");

// fire runs a handler like the browser: it waits for everything given to waitUntil.
async function fire(type, event) {
  const waits = [];
  listeners[type]({ ...event, waitUntil: (p) => waits.push(p) });
  await Promise.all(waits);
}

const pushData = (text) => (text === null ? null : { json: () => JSON.parse(text) });

const pushCases = [
  {
    name: "card",
    data: '{"title":"Maanden","body":"enero","url":"/#/kaart/3/1-2","tag":"leitner-card"}',
    title: "Maanden",
    body: "enero",
    url: "/#/kaart/3/1-2",
    tag: "leitner-card",
  },
  { name: "no tag", data: '{"title":"Leitner","body":"test","url":"/"}', title: "Leitner", body: "test", url: "/", tag: undefined },
  { name: "broken JSON", data: "{nope", title: "Leitner", body: "", url: "/", tag: undefined },
  { name: "no data", data: null, title: "Leitner", body: "", url: "/", tag: undefined },
  { name: "JSON null", data: "null", title: "Leitner", body: "", url: "/", tag: undefined },
  { name: "wrong types", data: '{"title":5,"body":[],"url":{},"tag":1}', title: "Leitner", body: "", url: "/", tag: undefined },
];
for (const c of pushCases) {
  shown.length = 0;
  await fire("push", { data: pushData(c.data) });
  const [title, opts] = shown[0] ?? [];
  check(`push ${c.name}: one notification`, shown.length === 1, `${shown.length}`);
  check(`push ${c.name}: title`, title === c.title, title);
  check(`push ${c.name}: body`, opts?.body === c.body, opts?.body);
  check(`push ${c.name}: url`, opts?.data?.url === c.url, opts?.data?.url);
  check(`push ${c.name}: tag`, opts?.tag === c.tag && opts?.renotify === (c.tag ? true : undefined), `${opts?.tag} ${opts?.renotify}`);
}

// A browser that refuses the options still shows the notification.
shown.length = 0;
refuseOptions = true;
await fire("push", { data: pushData(pushCases[0].data) });
refuseOptions = false;
check("refused options: plain notification", shown.length === 1 && shown[0][1].body === "enero" && !shown[0][1].tag,
  JSON.stringify(shown));

// Clicks.
const base = "https://leitner.example.com:8443";
function fakeWindow(url, { navigate = "ok" } = {}) {
  return {
    url,
    focused: false,
    navigated: null,
    messages: [],
    async focus() {
      this.focused = true;
    },
    async navigate(to) {
      if (navigate === "throw") {
        throw new TypeError("not controlled");
      }
      this.navigated = to;
      return this;
    },
    postMessage(m) {
      this.messages.push(m);
    },
  };
}
async function click(url) {
  let closed = false;
  await fire("notificationclick", { notification: { data: url === undefined ? undefined : { url }, close: () => (closed = true) } });
  check(`click ${url}: closed`, closed);
}

windows = [];
opened.length = 0;
await click("/#/kaart/3/1-2");
check("no window: opens one", opened.length === 1 && opened[0] === base + "/#/kaart/3/1-2", JSON.stringify(opened));

opened.length = 0;
await click("https://evil.example.com/x");
check("foreign url: start page", opened[0] === base + "/", JSON.stringify(opened));
opened.length = 0;
await click(undefined);
check("no data: start page", opened[0] === base + "/", JSON.stringify(opened));
opened.length = 0;
await click("http://[bad");
check("bad url: start page", opened[0] === base + "/", JSON.stringify(opened));

let w = fakeWindow(base + "/#/lijst/1");
windows = [w];
opened.length = 0;
await click("/#/kaart/4/2-1");
check("open window: focused and navigated", w.focused && w.navigated === base + "/#/kaart/4/2-1" && opened.length === 0,
  JSON.stringify(w));
check("open window: no message", w.messages.length === 0);

w = fakeWindow(base + "/#/lijst/1", { navigate: "throw" });
windows = [w];
await click("/#/kaart/5/1-2");
check("no navigate: asks the page", w.messages.length === 1 && w.messages[0].type === "open" && w.messages[0].url === base + "/#/kaart/5/1-2",
  JSON.stringify(w.messages));

const same = fakeWindow(base + "/#/kaart/6/1-2");
windows = [fakeWindow(base + "/"), same];
await click("/#/kaart/6/1-2");
check("window already there: focused, asked to start again",
  same.focused && same.navigated === null && same.messages.length === 1 && same.messages[0].url === base + "/#/kaart/6/1-2",
  JSON.stringify(same));

if (failures.length) {
  for (const f of failures) {
    print("FAIL " + f);
  }
  throw new Error(`${failures.length} sw.js test(s) failed`);
}
