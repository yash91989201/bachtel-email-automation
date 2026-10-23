// AI setup discovery adapter. The preset setup form owns its state through
// this Alpine factory: fetching the model list is read-only, a stale response
// never replaces models for changed inputs, and the server-rendered catalog
// stays the fallback list after any edit.
window.aiSetup = function (el) {
  return {
    loading: false,
    error: "",
    status: "",
    format: "",
    seq: 0,
    models: [],
    fallback: [],

    init() {
      this.fallback = this.parseModels(el.dataset.models);
      this.models = this.fallback;
      const select = el.querySelector('select[name="model"]');
      this.format = this.formatOf(select ? select.value : el.dataset.model || "");
    },

    // pick refreshes the read-only inferred format. The server resolves the
    // format again on validation; this display never selects a wire contract.
    pick() {
      const select = el.querySelector('select[name="model"]');
      this.format = select ? this.formatOf(select.value) : "";
    },

    parseModels(raw) {
      try {
        const parsed = JSON.parse(raw || "[]");
        return Array.isArray(parsed) ? parsed : [];
      } catch (e) {
        return [];
      }
    },

    formatOf(id) {
      if (!id) return "";
      const model = this.models.find((option) => option.id === id && option.supported);
      return model ? model.format_label : "";
    },

    label(model) {
      if (model.format_label) return model.id + " · " + model.format_label;
      if (model.reason) return model.id + " — " + model.reason;
      return model.id;
    },

    render() {
      const select = el.querySelector('select[name="model"]');
      if (!select) return;
      const selected = select.value;
      const options = [new Option("Choose a model", "", false, false)];
      for (const model of this.models) {
        const option = new Option(this.label(model), model.id, false, model.id === selected);
        option.disabled = !model.supported;
        options.push(option);
      }
      select.replaceChildren(...options);
      if (selected) select.value = selected;
      this.format = this.formatOf(select.value);
    },

    // Any root or key edit invalidates the selection, the inferred format and
    // any in-flight request. The server-rendered list is restored so a saved
    // model can still be chosen without another fetch.
    changed() {
      this.seq += 1;
      this.loading = false;
      this.error = "";
      this.status = "";
      this.models = this.fallback;
      const select = el.querySelector('select[name="model"]');
      if (select) select.value = "";
      this.format = "";
      this.render();
    },

    restore() {
      const root = el.querySelector('input[name="api_root"]');
      if (root) {
        root.value = el.dataset.defaultRoot || "";
        this.changed();
      }
    },

    async fetchModels() {
      const root = el.querySelector('input[name="api_root"]');
      const key = el.querySelector('input[name="api_key"]');
      const seq = (this.seq += 1);
      this.loading = true;
      this.error = "";
      this.status = "";
      try {
        const body = new URLSearchParams({
          provider: el.dataset.provider || "",
          api_root: root ? root.value : "",
          api_key: key ? key.value : "",
        });
        const response = await fetch(el.dataset.endpoint, {
          method: "POST",
          headers: { "Content-Type": "application/x-www-form-urlencoded", Accept: "application/json" },
          body: body,
        });
        const data = await response.json();
        // A late response never replaces models for changed inputs.
        if (seq !== this.seq) return;
        this.loading = false;
        if (!response.ok || !Array.isArray(data.models)) {
          this.error = data && data.error ? data.error : "The model list could not be fetched.";
          this.models = this.fallback;
          this.status = "";
          this.render();
          return;
        }
        this.models = data.models;
        this.error = "";
        this.status = data.models.length
          ? data.models.length + " models listed. Choose one."
          : "This endpoint listed no models. Choose a Custom compatible connection to enter the exact model ID.";
        const select = el.querySelector('select[name="model"]');
        if (select) select.value = "";
        this.render();
      } catch (e) {
        if (seq !== this.seq) return;
        this.loading = false;
        this.error = "The model list could not be fetched.";
        this.models = this.fallback;
        this.status = "";
        this.render();
      }
    },
  };
};
