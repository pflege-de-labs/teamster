// Swaps a record page between its view and edit parts without a reload, warns about unsaved
// edits, and swaps the Team and channel IDs in the summary for their names.
(function (global) {
  global.teamsterRecordURL = (href, editing) => {
    const u = new URL(href);
    const rest = u.search.slice(1).split("&").filter((p) => p && p.split("=")[0] !== "edit");
    if (editing) rest.push("edit");
    u.search = rest.join("&");
    return u.toString();
  };
  global.teamsterNameOf = (items, id) => items.find((i) => i.id === id)?.name ?? null;
  // Compares serialised submittable state, so buttons never count and script-set hidden values do.
  global.teamsterFormDirty = (baseline, current) => baseline !== current;
  global.teamsterFormState = (formData) => new URLSearchParams(formData).toString();
  if (typeof document === "undefined") return;

  const record = document.querySelector("[data-record]");
  if (record) {
    const view = record.querySelector('[data-mode="view"]');
    const edit = record.querySelector('[data-mode="edit"]');
    const form = edit && edit.querySelector("form");
    let submitting = false;
    const state = () => global.teamsterFormState(new FormData(form));
    const baseline = form ? state() : "";
    // Hidden inputs are written by pickers.js, so reset() does not bring them back.
    const hiddens = form ? [...form.elements].filter((f) => f.name).map((f) => [f, f.value]) : [];
    const show = (editing) => {
      view.hidden = editing;
      edit.hidden = !editing;
      history.replaceState(null, "", global.teamsterRecordURL(location.href, editing));
    };
    // A new page's Cancel also carries the attribute, but has no record to swap.
    if (view && edit && form) {
      record.addEventListener("click", (event) => {
        const el = event.target.closest?.("[data-record-edit], [data-record-cancel]");
        if (!el || !record.contains(el)) return;
        event.preventDefault();
        if (el.hasAttribute("data-record-cancel")) {
          form.reset();
          hiddens.forEach(([f, v]) => {
            if (f.type === "hidden") f.value = v;
          });
        }
        show(el.hasAttribute("data-record-edit"));
        if (!edit.hidden) form.querySelector("input:not([type=hidden]), select, textarea")?.focus();
      });
      form.addEventListener("submit", (event) => {
        submitting = true;
        // Another handler may cancel the submit, after which leaving is unsaved again.
        setTimeout(() => {
          if (event.defaultPrevented) submitting = false;
        }, 0);
      });
      window.addEventListener("beforeunload", (event) => {
        if (submitting || edit.hidden || !global.teamsterFormDirty(baseline, state())) return;
        event.preventDefault();
        event.returnValue = "";
      });
    }
  }

  async function names(selector, attr, url) {
    for (const el of document.querySelectorAll(selector)) {
      try {
        const res = await fetch(url(el));
        if (!res.ok) continue;
        const items = Object.values(await res.json())[0];
        const name = global.teamsterNameOf(items, el.dataset[attr]);
        if (name === null) continue;
        const id = document.createElement("span");
        id.className = "text-xs text-slate-500";
        id.textContent = el.textContent;
        el.textContent = name + " ";
        el.appendChild(id);
      } catch (e) {
        // The ID stays when Graph is unreachable.
      }
    }
  }
  const teams = "/api/graph/teams";
  names("[data-team-id]:not([data-channel-id])", "teamId", () => teams);
  names("[data-channel-id]", "channelId", (el) => teams + "/" + encodeURIComponent(el.dataset.teamId) + "/channels");
})(typeof window !== "undefined" ? window : globalThis);
