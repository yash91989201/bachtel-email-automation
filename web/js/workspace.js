// Workspace adapter: connects the owned tabs and collapsible primitives to
// the URL fragment and to htmx polling swaps, without holding a second source
// of truth. The active tab stays the primitive's own state; this file only
// reads and writes the fragment and re-applies that state after a swap.
(function () {
  "use strict";

  const TABS = "[data-workspace-tabs]";
  const TRIGGER = '[data-slot="tabs-trigger"]';
  const PERSIST = "[data-workspace-persist]";

  // Legacy bookmarks from the split campaign/task/sending pages. Each entry
  // names the canonical section and the region to reveal, so an old link opens
  // the same evidence without a second request or any mutation.
  const LEGACY = {
    campaign: {
      preview: ["template", "preview-heading"],
      history: ["activity", "campaign-activity"],
    },
    task: {
      review: ["recipients", "task-recipients-summary"],
      preview: ["recipients", "task-preview"],
      rows: ["recipients", "task-row-reasons"],
      send: ["sending", "controls-heading"],
      recovery: ["sending", "task-recovery"],
      pacing: ["sending", "sending-settings"],
      recent: ["results", "task-results-history"],
      results: ["results", "task-results"],
    },
  };

  // openState remembers the operator's disclosure choice by collapsible id so
  // a polling swap of the disclosure's markup does not close it again.
  const openState = new Map();
  const accordionState = new Map();

  function triggersOf(root) {
    return [...root.querySelectorAll(TRIGGER)].filter((tab) => tab.closest(TABS) === root);
  }

  function hasValue(root, value) {
    return triggersOf(root).some((tab) => tab.getAttribute("data-templ-value") === value);
  }

  function activeValue(root) {
    const active = triggersOf(root).find((tab) => tab.hasAttribute("data-active"));
    return active ? active.getAttribute("data-templ-value") : null;
  }

  function requestedValue(root) {
    const requested = location.hash.slice(1);
    if (!requested) {
      const value = root.getAttribute("data-workspace-default");
      return hasValue(root, value) ? { value, target: null, legacy: false } : null;
    }
    if (hasValue(root, requested)) return { value: requested, target: null, legacy: false };
    const alias = LEGACY[root.id]?.[requested];
    if (alias && hasValue(root, alias[0])) {
      return { value: alias[0], target: alias[1], legacy: true };
    }
    return null;
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
    const value = requested?.value ?? activeValue(root);
    if (value) window.templ.tabs.setActive(root, value);
    if (requested?.legacy) {
      // Normalize the bookmark in place: no request, no state change.
      history.replaceState(null, "", "#" + requested.value);
    }
    if (requested?.target) {
      const target = document.getElementById(requested.target);
      if (target?.matches('[data-slot="accordion"]')) {
        const item = target.querySelector('[data-slot="accordion-item"]');
        const value = item?.getAttribute("data-templ-value");
        if (value && window.templ?.accordion) {
          window.templ.accordion.setValues(target, [value]);
          accordionState.set(target.id, [value]);
        }
      }
      target?.scrollIntoView({ block: "start" });
    }
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

  function restoreAccordions() {
    if (!window.templ?.accordion) return;
    for (const root of document.querySelectorAll(`${PERSIST}[data-slot="accordion"]`)) {
      if (root.id && accordionState.has(root.id)) {
        window.templ.accordion.setValues(root, accordionState.get(root.id));
      }
    }
  }

  function syncAll() {
    syncTabs();
    restoreCollapsibles();
    restoreAccordions();
  }

  // The tabs runtime handles the click first (it registered its listener
  // earlier); push the selected section so Back/Forward return to the prior
  // one instead of replacing the same history entry.
  document.addEventListener("click", (event) => {
    const trigger = event.target instanceof Element ? event.target.closest(TRIGGER) : null;
    const root = trigger?.closest(TABS);
    if (!root || !trigger) return;
    const value = trigger.getAttribute("data-templ-value");
    if (value && location.hash.slice(1) !== value) {
      history.pushState(null, "", "#" + value);
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

  // htmx replaces the polling region's markup; the tabs root and its triggers
  // stay in place, where the swapped panels need the active state re-applied.
  // afterSwap corrects the new panels immediately, and afterSettle repeats the
  // correction because htmx rewrites the swapped attributes while settling.
  document.addEventListener("htmx:afterSwap", syncAll);
  document.addEventListener("htmx:afterSettle", syncAll);
  // Back/forward between workspace fragments keeps the visible section in step
  // with the URL instead of leaving the previous selection on screen.
  window.addEventListener("hashchange", syncTabs);
  window.addEventListener("popstate", syncTabs);

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
    document.addEventListener("DOMContentLoaded", syncAll);
  } else {
    syncAll();
  }
})();
