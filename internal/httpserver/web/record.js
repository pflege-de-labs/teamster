// Swaps a record page between its view and edit parts without a reload, warns about unsaved
// edits, and swaps the Team and channel IDs in the summary for their names.
(function (global) {
  // The page may be served at another URL (a failed save), so URLs come from data-record.
  global.teamsterRecordURL = (href, editing) => (editing ? href + "?edit" : href);
  global.teamsterNameOf = (items, id) => items.find((i) => i.id === id)?.name ?? null;
  if (typeof document === "undefined") return;

  const record = document.querySelector("[data-record]");
  if (record) {
    const view = record.querySelector('[data-mode="view"]');
    const edit = record.querySelector('[data-mode="edit"]');
    const form = edit && edit.querySelector("form");
    let submitting = false;
    // Events only, because pickers.js rewrites fields without firing any.
    let dirty = false;
    const show = (editing) => {
      view.hidden = editing;
      edit.hidden = !editing;
      history.replaceState(null, "", global.teamsterRecordURL(record.dataset.record, editing));
    };
    // A new page's Cancel also carries the attribute, but has no record to swap.
    if (view && edit && form) {
      record.addEventListener("click", (event) => {
        const el = event.target.closest?.("[data-record-edit], [data-record-cancel]");
        if (!el || !record.contains(el)) return;
        event.preventDefault();
        if (el.hasAttribute("data-record-cancel") && dirty) {
          // Reload so the pickers rebuild from the stored IDs.
          dirty = false;
          location.replace(global.teamsterRecordURL(record.dataset.record, false));
          return;
        }
        show(el.hasAttribute("data-record-edit"));
        if (!edit.hidden) form.querySelector("input:not([type=hidden]), select, textarea")?.focus();
      });
      form.addEventListener("input", () => {
        dirty = true;
      });
      form.addEventListener("change", () => {
        dirty = true;
      });
      form.addEventListener("submit", (event) => {
        submitting = true;
        // Another handler may cancel the submit, after which leaving is unsaved again.
        setTimeout(() => {
          if (event.defaultPrevented) submitting = false;
        }, 0);
      });
      window.addEventListener("beforeunload", (event) => {
        if (submitting || edit.hidden || !dirty) return;
        event.preventDefault();
        event.returnValue = "";
      });
    }
  }

  async function names(selector, attr, key, url) {
    for (const el of document.querySelectorAll("[data-record] " + selector)) {
      try {
        const res = await fetch(url(el));
        if (!res.ok) continue;
        const items = (await res.json())[key];
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
  names("[data-team-id]:not([data-channel-id])", "teamId", "teams", () => teams);
  names("[data-channel-id]", "channelId", "channels", (el) => teams + "/" + encodeURIComponent(el.dataset.teamId) + "/channels");
})(typeof window !== "undefined" ? window : globalThis);
