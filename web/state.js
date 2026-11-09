// The user's tree as the app uses it, built from GET /api/tree.

// ROOT is the key of the top level in children().
export const ROOT = "root";

// itemKey identifies a child across both kinds, e.g. "heading:3".
export function itemKey(item) {
  return `${item.kind}:${item.id}`;
}

export class Library {
  constructor(tree) {
    this.headings = new Map(tree.headings.map((h) => [h.id, h]));
    this.lists = new Map(tree.lists.map((l) => [l.id, l]));
    this.kids = new Map();
    for (const h of tree.headings) {
      this.#add(h.parentId ?? ROOT, { kind: "heading", id: h.id, position: h.position });
    }
    for (const l of tree.lists) {
      this.#add(l.headingId, { kind: "list", id: l.id, position: l.position });
    }
    for (const items of this.kids.values()) {
      items.sort((a, b) => a.position - b.position || a.kind.localeCompare(b.kind) || a.id - b.id);
    }
  }

  #add(parent, item) {
    if (!this.kids.has(parent)) {
      this.kids.set(parent, []);
    }
    this.kids.get(parent).push(item);
  }

  // children of parent (ROOT or a heading id), in the user's order.
  children(parent) {
    return this.kids.get(parent) ?? [];
  }

  name(item) {
    return (item.kind === "heading" ? this.headings : this.lists).get(item.id)?.name ?? "";
  }

  // setOrder applies a new order locally, before the server confirms it.
  setOrder(parent, items) {
    items.forEach((item, i) => {
      item.position = i + 1;
      const stored = (item.kind === "heading" ? this.headings : this.lists).get(item.id);
      if (stored) {
        stored.position = i + 1;
      }
    });
    this.kids.set(parent, items);
  }

  // ancestors of a heading, top level first, the heading itself last.
  path(headingId) {
    const path = [];
    for (let h = this.headings.get(headingId); h; h = this.headings.get(h.parentId)) {
      path.unshift(h);
    }
    return path;
  }

  // contents counts everything inside a heading, at every level.
  contents(headingId) {
    const count = { headings: 0, lists: 0 };
    const walk = (id) => {
      for (const item of this.children(id)) {
        if (item.kind === "heading") {
          count.headings++;
          walk(item.id);
        } else {
          count.lists++;
        }
      }
    };
    walk(headingId);
    return count;
  }

  // listsIn returns the ids of all lists inside a heading, at every level.
  listsIn(headingId) {
    const ids = [];
    const walk = (id) => {
      for (const item of this.children(id)) {
        if (item.kind === "heading") {
          walk(item.id);
        } else {
          ids.push(item.id);
        }
      }
    };
    walk(headingId);
    return ids;
  }

  // headingOptions lists all headings in tree order with their depth (0 at
  // the top), leaving out the subtree of skipId: a heading can't move there.
  headingOptions(skipId = null) {
    const out = [];
    const walk = (parent, depth) => {
      for (const item of this.children(parent)) {
        if (item.kind !== "heading" || item.id === skipId) {
          continue;
        }
        out.push({ id: item.id, name: this.name(item), depth });
        walk(item.id, depth + 1);
      }
    };
    walk(ROOT, 0);
    return out;
  }
}
