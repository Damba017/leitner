// Drag to reorder, with mouse, pen or touch (Pointer Events), a simplified
// version of nestje's sortable. An item is dragged by its handle and only
// among its siblings. The dragged item follows the pointer with a transform
// and a line shows where it will land; the DOM itself only changes when
// onReorder re-renders, so nothing is pulled from under the pointer.
// Keyboard users have the up/down buttons instead; Escape cancels a drag.

const edge = 40; // px from the scroll edge where auto-scroll starts

// makeSortable handles drags inside root. onReorder(container, keys) gets the
// item's parent element and the new order of data-key values.
export function makeSortable(root, { itemSelector, handleSelector, onReorder }) {
  root.addEventListener("pointerdown", (down) => {
    const handle = down.target.closest(handleSelector);
    if (!handle || !root.contains(handle) || down.button !== 0) {
      return;
    }
    const item = handle.closest(itemSelector);
    const container = item?.parentElement;
    if (!container) {
      return;
    }
    down.preventDefault();
    const siblings = [...container.children].filter((c) => c.matches(itemSelector));
    const from = siblings.indexOf(item);
    const scroller = scrollParent(container);
    const startY = down.clientY;
    const startScroll = scroller.scrollTop;
    let target = from;
    let marked = null;

    handle.setPointerCapture(down.pointerId);
    item.classList.add("is-dragging");
    document.body.classList.add("is-dragging");

    const mark = (node, cls) => {
      marked?.classList.remove("drop-before", "drop-after");
      marked = node;
      node?.classList.add(cls);
    };

    const move = (e) => {
      if (e.pointerId !== down.pointerId) {
        return;
      }
      autoScroll(scroller, e.clientY);
      item.style.transform = `translateY(${e.clientY - startY + scroller.scrollTop - startScroll}px)`;
      // The target is the number of other siblings whose middle is above the pointer.
      const others = siblings.filter((s) => s !== item);
      let index = 0;
      for (const s of others) {
        const r = s.getBoundingClientRect();
        if (e.clientY > r.top + r.height / 2) {
          index++;
        }
      }
      target = index;
      if (target === from) {
        mark(null);
      } else if (target < others.length) {
        mark(others[target], "drop-before");
      } else {
        mark(others[others.length - 1], "drop-after");
      }
    };

    const finish = (apply) => {
      handle.removeEventListener("pointermove", move);
      handle.removeEventListener("pointerup", up);
      handle.removeEventListener("pointercancel", cancel);
      handle.removeEventListener("lostpointercapture", cancel);
      document.removeEventListener("keydown", key, true);
      if (handle.hasPointerCapture(down.pointerId)) {
        handle.releasePointerCapture(down.pointerId);
      }
      mark(null);
      item.style.transform = "";
      item.classList.remove("is-dragging");
      document.body.classList.remove("is-dragging");
      if (apply && target !== from) {
        const keys = siblings.map((s) => s.dataset.key);
        const [moved] = keys.splice(from, 1);
        keys.splice(target, 0, moved);
        onReorder(container, keys);
      }
    };
    const up = (e) => e.pointerId === down.pointerId && finish(true);
    const cancel = (e) => e.pointerId === down.pointerId && finish(false);
    const key = (e) => {
      if (e.key === "Escape") {
        e.preventDefault();
        target = from;
        finish(false);
      }
    };

    handle.addEventListener("pointermove", move);
    handle.addEventListener("pointerup", up);
    handle.addEventListener("pointercancel", cancel);
    handle.addEventListener("lostpointercapture", cancel);
    document.addEventListener("keydown", key, true);
  });
}

// scrollParent is the nearest element that scrolls vertically (the desktop
// sidebar), or the page itself.
function scrollParent(node) {
  for (let n = node; n && n !== document.body; n = n.parentElement) {
    const overflow = getComputedStyle(n).overflowY;
    if ((overflow === "auto" || overflow === "scroll") && n.scrollHeight > n.clientHeight) {
      return n;
    }
  }
  return document.scrollingElement;
}

function autoScroll(scroller, y) {
  const isPage = scroller === document.scrollingElement;
  const top = isPage ? 0 : scroller.getBoundingClientRect().top;
  const bottom = isPage ? window.innerHeight : scroller.getBoundingClientRect().bottom;
  if (y < top + edge) {
    scroller.scrollTop -= 12;
  } else if (y > bottom - edge) {
    scroller.scrollTop += 12;
  }
}
