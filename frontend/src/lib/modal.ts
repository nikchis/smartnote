// Modal windows: a CSS backdrop stops the mouse but not the keyboard, so the
// rest of the window is made inert while a modal is open. Only the modal
// opened last takes input; the others wait under it.

const open: HTMLElement[] = [];
let inerted: Element[] = [];

/** refresh makes everything but the top modal and its ancestors inert. */
function refresh(): void {
  for (const el of inerted) el.removeAttribute("inert");
  inerted = [];
  const top = open.at(-1);
  for (let el = top; el?.parentElement && el !== document.body; el = el.parentElement) {
    for (const sibling of el.parentElement.children) {
      if (sibling === el || sibling.hasAttribute("inert")) continue;
      sibling.setAttribute("inert", "");
      inerted.push(sibling);
    }
  }
}

function focusInside(node: HTMLElement): void {
  if (node.contains(document.activeElement)) return;
  const first = node.querySelector<HTMLElement>(
    "[autofocus], input:not([disabled]), textarea:not([disabled]), button:not([disabled])",
  );
  if (first) {
    first.focus();
    return;
  }
  node.tabIndex = -1;
  node.focus();
}

/**
 * modal is a Svelte action for the root element of a modal window: while it
 * is mounted, the rest of the window takes no focus, keys or clicks. When it
 * goes, the focus returns to where it was.
 */
export function modal(node: HTMLElement): { destroy(): void } {
  const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  open.push(node);
  refresh();
  focusInside(node);
  return {
    destroy() {
      const i = open.indexOf(node);
      if (i >= 0) open.splice(i, 1);
      refresh();
      if (previous?.isConnected && !previous.closest("[inert]")) previous.focus();
    },
  };
}
