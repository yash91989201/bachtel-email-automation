// AI setup discovery adapter. The preset setup form owns its state through
// this Alpine factory: fetching the model list is read-only, a stale response
// never replaces models for changed inputs, and every listed ID is selectable
// but unvalidated until the server validates the exact configuration.
window.aiSetup = function (el) {
  return {
    loading: false,
    error: "",
    status: "",
    seq: 0,
    models: [],
    fallback: [],

    init() {
      this.fallback = this.parseModels(el.dataset.models);
      this.models = this.fallback;
      this.render();
    },

    parseModels(raw) {
      try {
        const parsed = JSON.parse(raw || "[]");
        return Array.isArray(parsed) ? parsed : [];
      } catch (e) {
        return [];
      }
    },

    render() {
      const select = el.querySelector('select[name="model"]');
      if (!select) return;
      const selected = select.value;
      const options = [new Option("Choose a model", "", false, false)];
      for (const model of this.models) {
        options.push(new Option(model.id, model.id, false, model.id === selected));
      }
      select.replaceChildren(...options);
      if (selected) select.value = selected;
    },

    // Any root or key edit invalidates the selection and any in-flight
    // request. The server-rendered choice is restored so a saved or submitted
    // model can still be chosen without another fetch; nothing is fetched or
    // inferred again.
    changed() {
      this.seq += 1;
      this.loading = false;
      this.error = "";
      this.status = "";
      this.models = this.fallback;
      const select = el.querySelector('select[name="model"]');
      if (select) select.value = "";
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
          ? data.models.length + " models listed. Every ID is selectable; compatibility is established by validation."
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
