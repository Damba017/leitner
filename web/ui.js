// Small DOM helpers shared by the app's modules.

// el creates an element. props sets attributes, except "text" (textContent)
// and on* functions (event listeners); null, undefined and false are left
// out. children are nodes or strings (as text, never HTML).
export function el(tag, props = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (value === null || value === undefined || value === false) {
      continue;
    }
    if (key === "text") {
      node.textContent = value;
    } else if (key.startsWith("on") && typeof value === "function") {
      node.addEventListener(key.slice(2), value);
    } else {
      node.setAttribute(key, value === true ? "" : String(value));
    }
  }
  node.append(...children.flat().filter((c) => c !== null && c !== undefined && c !== false));
  return node;
}

// showStatus shows a message above the page; error ones in red. An empty
// message hides it.
export function showStatus(message, isError = false) {
  const status = document.getElementById("status");
  status.textContent = message;
  status.classList.toggle("is-error", isError);
  status.setAttribute("role", isError ? "alert" : "status");
  status.hidden = !message;
}
