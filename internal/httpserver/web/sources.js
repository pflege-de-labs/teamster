// Narrows the route form's template picker to the templates that handle the
// webhook its selector pins with teamster_source (ADR 0053). The server
// checks the same thing on save; this only keeps the wrong choice out of reach.
(function () {
  const form = document.querySelector('form[action="/admin/routes"]');
  if (!form) return;
  const selector = form.querySelector('[name="label_selector"]');
  const picker = form.querySelector('select[name="template_id"]');
  if (!selector || !picker) return;

  function pinnedSource() {
    try {
      const parsed = JSON.parse(selector.value || "{}");
      return typeof parsed.teamster_source === "string" ? parsed.teamster_source : "";
    } catch {
      // Half-typed JSON pins nothing yet; keep every template on offer.
      return "";
    }
  }

  function filter() {
    const source = pinnedSource();
    for (const option of picker.options) {
      const sources = option.dataset.sources;
      const hidden = source !== "" && sources !== undefined && !sources.split(",").includes(source);
      option.hidden = hidden;
      option.disabled = hidden && !option.selected;
    }
  }

  selector.addEventListener("input", filter);
  filter();
})();
