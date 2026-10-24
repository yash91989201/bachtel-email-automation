// Browser-controller checks for the AI setup discovery adapter. The adapter is
// loaded from its shipped source into a minimal DOM stub; these checks cover
// the state transitions that matter: every listed ID selectable, edits
// invalidating the fetched list, late responses ignored, and failures leaving
// the server-rendered choice intact.
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";
import vm from "node:vm";

const source = await readFile(new URL("../web/js/ai-setup.js", import.meta.url), "utf8");

class FakeOption {
  constructor(text, value, defaultSelected, selected) {
    this.text = text;
    this.value = value;
    this.selected = Boolean(selected || defaultSelected);
    this.disabled = false;
  }
}

function form({ models = [], selectedModel = "", endpoint = "/account/ai/models", defaultRoot = "https://default.test/v1" } = {}) {
  const select = {
    value: selectedModel,
    options: [],
    replaceChildren(...options) {
      this.options = options;
      const selected = options.find((option) => option.selected);
      this.value = selected ? selected.value : "";
    },
  };
  const root = { value: "" };
  const key = { value: "" };
  const el = {
    dataset: { models: JSON.stringify(models), endpoint, provider: "opencode-go", defaultRoot },
    querySelector(selector) {
      switch (selector) {
        case 'select[name="model"]':
          return select;
        case 'input[name="api_root"]':
          return root;
        case 'input[name="api_key"]':
          return key;
        default:
          return null;
      }
    },
  };
  return { el, select, root, key };
}

function load(fetchImpl) {
  const context = vm.createContext({ window: {}, Option: FakeOption, fetch: fetchImpl, URLSearchParams });
  vm.runInContext(source, context, { filename: "ai-setup.js" });
  return context.window.aiSetup;
}

const values = (select) => select.options.map((option) => option.value);
const ids = (models) => Array.from(models, (model) => model.id);

test("init keeps the server-rendered saved model selectable", () => {
  const aiSetup = load(() => {
    throw new Error("unexpected fetch");
  });
  const { el, select } = form({ models: [{ id: "retired-model" }], selectedModel: "retired-model" });
  aiSetup(el).init();

  assert.deepEqual(values(select), ["", "retired-model"]);
  assert.equal(select.value, "retired-model");
  assert.equal(select.options[1].disabled, false);
});

test("fetch lists every returned ID as an enabled, unvalidated choice", async () => {
  const payload = { models: [{ id: "brand-new-model" }, { id: "mystery-model" }], error: "" };
  const aiSetup = load(async () => ({ ok: true, json: async () => payload }));
  const { el, select } = form({ models: [{ id: "saved-model" }], selectedModel: "saved-model" });
  const controller = aiSetup(el);
  controller.init();

  await controller.fetchModels();

  assert.deepEqual(values(select), ["", "brand-new-model", "mystery-model"]);
  assert.ok(select.options.every((option) => !option.disabled));
  assert.equal(select.value, "");
  assert.match(controller.status, /Every ID is selectable/);
  assert.equal(controller.error, "");
});

test("edits invalidate the fetched list and a late response is ignored", async () => {
  let respond;
  const pending = new Promise((resolve) => {
    respond = resolve;
  });
  const aiSetup = load(() => pending);
  const { el, select, root } = form({ models: [{ id: "saved-model" }], selectedModel: "saved-model" });
  const controller = aiSetup(el);
  controller.init();

  const fetching = controller.fetchModels();
  root.value = "https://edited.test/v1";
  controller.changed();
  assert.deepEqual(values(select), ["", "saved-model"]);
  assert.equal(select.value, "");

  respond({ ok: true, json: async () => ({ models: [{ id: "stale-model" }], error: "" }) });
  await fetching;

  assert.deepEqual(ids(controller.models), ["saved-model"]);
  assert.deepEqual(values(select), ["", "saved-model"]);
  assert.equal(controller.status, "");
  assert.equal(controller.error, "");
  assert.equal(controller.loading, false);
});

test("a rejected fetch restores the server-rendered choice and reports safely", async () => {
  const failure = "The endpoint did not return a usable model list.";
  const aiSetup = load(async () => ({ ok: false, json: async () => ({ models: [], error: failure }) }));
  const { el, select } = form({ models: [{ id: "submitted-model" }], selectedModel: "submitted-model" });
  const controller = aiSetup(el);
  controller.init();

  await controller.fetchModels();

  assert.deepEqual(ids(controller.models), ["submitted-model"]);
  assert.deepEqual(values(select), ["", "submitted-model"]);
  assert.equal(select.options[1].disabled, false);
  assert.equal(controller.error, failure);
  assert.equal(controller.status, "");
});

test("a network failure leaves the form usable with no stale list", async () => {
  const aiSetup = load(async () => {
    throw new Error("offline");
  });
  const { el, select } = form();
  const controller = aiSetup(el);
  controller.init();

  await controller.fetchModels();

  assert.deepEqual(values(select), [""]);
  assert.equal(controller.error, "The model list could not be fetched.");
  assert.equal(ids(controller.models).length, 0);
});
