// Workspace adapter: connects the owned tabs and collapsible primitives to
// the URL fragment and to htmx polling swaps, without holding a second source
// of truth. The active tab stays the primitive's own state; this file only
// reads and writes the fragment and re-applies that state after a swap.
(function () {
  "use strict";

  const TABS = "[data-workspace-tabs]";
  const TRIGGER = '[data-slot="tabs-trigger"]';
  const PERSIST = "[data-workspace-persist]";

  // openState remembers the operator's disclosure choice by collapsible id so
  // a polling swap of the disclosure's markup does not close it again.
  const openState = new Map();

  function triggersOf(root) {
    return [...root.querySelectorAll(TRIGGER)].filter((tab) => tab.closest(TABS) === root);
  }

  function activeValue(root) {
    const active = triggersOf(root).find((tab) => tab.hasAttribute("data-active"));
    return active ? active.getAttribute("data-templ-value") : null;
  }

  function requestedValue(root) {
    const requested = location.hash.slice(1);
    return triggersOf(root).some((tab) => tab.getAttribute("data-templ-value") === requested)
      ? requested
      : null;
  }

  // syncRoot applies the fragment when it names one of this workspace's tabs,
  // otherwise keeps the server-rendered (or previously selected) tab. New
  // panels arrive from a swap marked with the server default, so re-applying
  // the active value keeps the visible panel and the triggers in agreement.
  // A root with data-workspace-ignore-hash renders the panel the server chose
  // (a validation error), so the fragment never overrides that choice.
  function syncRoot(root) {
    if (!window.templ?.tabs) return;
    const requested = root.hasAttribute("data-workspace-ignore-hash") ? null : requestedValue(root);
    const value = requested ?? activeValue(root);
    if (value) window.templ.tabs.setActive(root, value);
  }

  function syncTabs() {
    document.querySelectorAll(TABS).forEach(syncRoot);
  }

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

  function syncAll() {
    syncTabs();
    restoreCollapsibles();
  }

  // The tabs runtime handles the click first (it registered its listener
  // earlier); record the fragment for the tab the operator just selected.
  document.addEventListener("click", (event) => {
    const trigger = event.target instanceof Element ? event.target.closest(TRIGGER) : null;
    const root = trigger?.closest(TABS);
    if (!root || !trigger) return;
    const value = trigger.getAttribute("data-templ-value");
    if (value && location.hash.slice(1) !== value) {
      history.replaceState(null, "", "#" + value);
    }
  });

  // A link that reloads the workspace keeps the operator's selected section:
  // its href is the page, and the fragment is filled in from the active tab.
  document.addEventListener(
    "click",
    (event) => {
      const link = event.target instanceof Element ? event.target.closest("[data-workspace-hash-link]") : null;
      const root = link?.closest(TABS);
      const value = root && activeValue(root);
      if (value) link.href = link.getAttribute("href").split("#")[0] + "#" + value;
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

  // htmx replaces the polling region's markup; the tabs root and its triggers
  // stay in place, where the swapped panels need the active state re-applied.
  // afterSwap corrects the new panels immediately, and afterSettle repeats the
  // correction because htmx rewrites the swapped attributes while settling.
  document.addEventListener("htmx:afterSwap", syncAll);
  document.addEventListener("htmx:afterSettle", syncAll);
  // Back/forward between workspace fragments keeps the visible section in step
  // with the URL instead of leaving the previous selection on screen.
  window.addEventListener("hashchange", syncTabs);

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", syncAll);
  } else {
    syncAll();
  }
})();
