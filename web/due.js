// Practice questions and due counts from the offline snapshot (GET
// /api/offline), the same as the server answers them online: see
// PracticeItems and DueCounts in internal/library/practice.go. No DOM, so
// testdata/due_test.js can test it.

const DIRECTIONS = ["1-2", "2-1"];

// asks says whether a list with direction listDir asks direction d.
const asks = (listDir, d) => listDir === "both" || listDir === d;

function scheduleIndex(snapshot) {
  return new Map(snapshot.schedule.map((s) => [`${s.cardId}:${s.direction}`, s]));
}

// dueBox is the box of a card direction that is due at now (ms), or 0 if it
// isn't. A direction that was never answered is new: box 1, due.
function dueBox(schedule, cardId, d, now) {
  const s = schedule.get(`${cardId}:${d}`);
  if (!s) {
    return 1;
  }
  return Date.parse(s.dueAt) <= now ? s.box : 0;
}

// practiceItems returns the questions of the lists listIds, unshuffled, like
// GET /api/practice. mode "due": every due direction the list asks, with its
// box; "free": every card once, with the list's direction.
export function practiceItems(snapshot, listIds, mode, now) {
  const lists = new Map(snapshot.lists.map((l) => [l.id, l]));
  const wanted = new Set(listIds);
  const schedule = scheduleIndex(snapshot);
  const items = [];
  for (const card of snapshot.cards) {
    const list = lists.get(card.listId);
    if (!list || !wanted.has(card.listId)) {
      continue;
    }
    if (mode === "free") {
      items.push({ ...card, direction: list.direction });
      continue;
    }
    for (const d of DIRECTIONS) {
      const box = asks(list.direction, d) ? dueBox(schedule, card.id, d, now) : 0;
      if (box) {
        items.push({ ...card, direction: d, box });
      }
    }
  }
  return items;
}

// dueCounts returns the number of due questions per list id, like GET
// /api/due; lists with nothing due are left out.
export function dueCounts(snapshot, now) {
  const lists = new Map(snapshot.lists.map((l) => [l.id, l]));
  const schedule = scheduleIndex(snapshot);
  const counts = new Map();
  for (const card of snapshot.cards) {
    const list = lists.get(card.listId);
    for (const d of DIRECTIONS) {
      if (list && asks(list.direction, d) && dueBox(schedule, card.id, d, now)) {
        counts.set(card.listId, (counts.get(card.listId) ?? 0) + 1);
      }
    }
  }
  return counts;
}
