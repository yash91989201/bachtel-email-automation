// Section-local interactivity for the campaign and task workspaces. Section
// navigation is ordinary page navigation, so this file only keeps disclosure
// choices across htmx swaps inside one page and invalidates a restored sending
// confirmation. It holds no URL or history state.
(function () {
  "use strict";

  const PERSIST = "[data-workspace-persist]";

  // openState remembers the operator's disclosure choice by collapsible id so
  // a polling swap of the disclosure's markup does not close it again.
  const openState = new Map();
  const accordionState = new Map();

  // persistKey names a persisted collapsible by its trigger's target id, which
  // the server renders from the disclosure id. The root's own id is generated
  // per render, so it cannot identify the section across an htmx swap.
  function persistKey(root) {
    const trigger = root.querySelector('[data-slot="collapsible-trigger"]');
    return trigger?.getAttribute("data-templ-controls") || root.id;
  }

  function restoreCollapsibles() {
    if (!window.templ?.collapsible) return;
    for (const root of document.querySelectorAll(PERSIST)) {
      const key = persistKey(root);
      if (!openState.has(key)) continue;
      window.templ.collapsible.setOpen(root, openState.get(key));
    }
  }

  function restoreAccordions() {
    if (!window.templ?.accordion) return;
    for (const root of document.querySelectorAll(`${PERSIST}[data-slot="accordion"]`)) {
      if (root.id && accordionState.has(root.id)) {
        window.templ.accordion.setValues(root, accordionState.get(root.id));
      }
    }
  }

  // An accordion can render open on the server (a refused settings save reopens
  // it) without firing a change event. Seed the remembered values from the
  // rendered items so the first polling swap keeps it open.
  function seedAccordionState() {
    for (const root of document.querySelectorAll(`${PERSIST}[data-slot="accordion"]`)) {
      if (!root.id || accordionState.has(root.id)) continue;
      const values = [];
      for (const item of root.querySelectorAll('[data-slot="accordion-item"][data-open]')) {
        const value = item.getAttribute("data-templ-value");
        if (value) values.push(value);
      }
      if (values.length) accordionState.set(root.id, values);
    }
  }

  function restoreAll() {
    seedAccordionState();
    restoreCollapsibles();
    restoreAccordions();
  }

  document.addEventListener(
    "accordion-value-change",
    (event) => {
      const root = event.target instanceof Element ? event.target.closest(`${PERSIST}[data-slot="accordion"]`) : null;
      if (root?.id && Array.isArray(event.detail?.values)) accordionState.set(root.id, [...event.detail.values]);
    },
    true,
  );

  // collapsible-open-change fires before the primitive applies the new state.
  document.addEventListener(
    "collapsible-open-change",
    (event) => {
      const root = event.target instanceof Element ? event.target.closest(PERSIST) : null;
      if (root) openState.set(persistKey(root), event.detail.open);
    },
    true,
  );

  // htmx replaces the polling region's markup, so the swapped disclosures need
  // their remembered state re-applied. afterSwap corrects them immediately and
  // afterSettle repeats the correction because htmx rewrites swapped
  // attributes while settling.
  document.addEventListener("htmx:afterSwap", restoreAll);
  document.addEventListener("htmx:afterSettle", restoreAll);

  // History can restore a checked sending confirmation from the browser cache.
  // Keep drafts and the selected type, but require confirmation on page entry.
  window.addEventListener("pageshow", () => {
    // Native form restoration finishes after pageshow dispatch.
    setTimeout(() => {
      for (const input of document.querySelectorAll('#sending-live input[type="checkbox"][name="confirmed"]')) {
        input.checked = false;
        input.dispatchEvent(new Event("change", { bubbles: true }));
      }
    }, 0);
  });

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", restoreAll);
  } else {
    restoreAll();
  }
})();
