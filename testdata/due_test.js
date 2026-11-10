// Table tests for web/due.js. Run by js_test.go as
// `jsc -m testdata/due_test.js`; prints each failure and throws if any.

import { practiceItems, dueCounts } from "../web/due.js";

const now = Date.parse("2026-11-10T12:00:00Z");
const card = (id, listId) => ({ id, listId, front: `f${id}`, back: `b${id}`, note: "", pronunciation: "" });
const entry = (cardId, direction, box, dueAt) => ({ cardId, direction, box, dueAt, reviewedAt: "2026-11-01T10:00:00Z" });

const snapshot = {
  lists: [
    { id: 1, headingId: 1, direction: "1-2" },
    { id: 2, headingId: 1, direction: "2-1" },
    { id: 3, headingId: 2, direction: "both" },
  ],
  cards: [card(10, 1), card(11, 1), card(12, 1), card(20, 2), card(30, 3), card(31, 3), card(99, 404)],
  schedule: [
    entry(10, "1-2", 3, "2026-11-10T11:59:59Z"), // due a second ago
    entry(11, "1-2", 2, "2026-11-10T12:00:00Z"), // due exactly now
    entry(12, "1-2", 4, "2026-11-11T00:00:00Z"), // due tomorrow
    entry(20, "1-2", 5, "2026-12-01T00:00:00Z"), // a direction list 2 doesn't ask
    entry(30, "1-2", 2, "2026-11-12T00:00:00Z"), // card 30: only 2-1 (new) is due
    entry(31, "1-2", 1, "2026-11-09T00:00:00Z"),
    entry(31, "2-1", 5, "2026-11-01T00:00:00Z"),
  ],
};

// questions shows items as "card:direction:box", in order.
const questions = (items) => items.map((it) => `${it.id}:${it.direction}:${it.box ?? "-"}`).join(" ");

// [name, list ids, mode, want]
const cases = [
  ["due, one direction", [1], "due", "10:1-2:3 11:1-2:2"],
  ["due, new card in a 2-1 list", [2], "due", "20:2-1:1"],
  ["due, both directions", [3], "due", "30:2-1:1 31:1-2:1 31:2-1:5"],
  ["due, several lists (a heading)", [1, 2, 3], "due", "10:1-2:3 11:1-2:2 20:2-1:1 30:2-1:1 31:1-2:1 31:2-1:5"],
  ["due, list without cards", [7], "due", ""],
  ["card of an unknown list is left out", [404], "due", ""],
  ["free, every card once with the list's direction", [1, 3], "free", "10:1-2:- 11:1-2:- 12:1-2:- 30:both:- 31:both:-"],
];

const failures = [];
for (const [name, ids, mode, want] of cases) {
  const got = questions(practiceItems(snapshot, ids, mode, now));
  if (got !== want) {
    failures.push(`${name}: got "${got}", want "${want}"`);
  }
}

const item = practiceItems(snapshot, [1], "due", now)[0];
if (item.front !== "f10" || item.back !== "b10" || item.listId !== 1 || item.note !== "" || item.pronunciation !== "") {
  failures.push(`item fields: ${JSON.stringify(item)}`);
}

const counts = [...dueCounts(snapshot, now)].map(([id, n]) => `${id}=${n}`).join(" ");
if (counts !== "1=2 2=1 3=3") {
  failures.push(`dueCounts = "${counts}", want "1=2 2=1 3=3"`);
}
const empty = dueCounts({ lists: [], cards: [], schedule: [] }, now);
if (empty.size !== 0) {
  failures.push(`dueCounts of nothing has ${empty.size} entries`);
}

for (const f of failures) {
  print(f);
}
if (failures.length) {
  throw new Error(`${failures.length} due test(s) failed`);
}
