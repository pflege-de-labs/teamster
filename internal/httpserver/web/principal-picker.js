// Adds a search box to the principal selects (select[data-principal-picker]); the select stays
// the form field, so without this script the form still works.
(function (global) {
  const kinds = ["user", "group", "idp_group", "role"];
  const rank = (kind) => {
    const i = kinds.indexOf(kind);
    return i < 0 ? kinds.length : i;
  };
  global.teamsterFilterPrincipals = (entries, query, limit = 50) => {
    const words = (query || "").toLowerCase().split(/\s+/).filter(Boolean);
    return entries
      .filter((e) => {
        const hay = (e.label + " " + (e.search || "")).toLowerCase();
        return words.every((w) => hay.includes(w));
      })
      .sort((a, b) => rank(a.kind) - rank(b.kind) || a.label.localeCompare(b.label))
      .slice(0, limit);
  };
  // The option an arrow key moves to; from none, down is the first and up the last.
  global.teamsterStepPrincipal = (active, step, n) => (active < 0 ? (step > 0 ? 0 : n - 1) : (active + step + n) % n);
  // The users a search found that the picker does not list yet.
  global.teamsterNewPrincipals = (entries, hits) => {
    const known = new Set(entries.map((e) => e.value));
    return (hits || [])
      .filter((h) => h && typeof h.value === "string" && h.value.startsWith("user:") && !known.has(h.value))
      .map((h) => ({ value: h.value, label: h.label || h.value, kind: "user", search: h.search || "", title: h.title || "" }));
  };
  if (typeof document === "undefined") return;

  let seq = 0;
  document.querySelectorAll("select[data-principal-picker]").forEach((select) => {
    const entries = [];
    const badges = {};
    select.querySelectorAll("option").forEach((o) => {
      if (!o.value) return;
      const kind = o.value.split(":")[0];
      badges[kind] = o.parentElement.label || kind;
      entries.push({ value: o.value, label: o.textContent.trim(), kind, search: o.dataset.search || "", title: o.title });
    });

    const id = "principal-picker-" + ++seq;
    const wrap = document.createElement("div");
    wrap.style.position = "relative";
    const input = document.createElement("input");
    input.type = "text";
    input.autocomplete = "off";
    input.className = "w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm shadow-sm";
    input.setAttribute("role", "combobox");
    input.setAttribute("aria-autocomplete", "list");
    input.setAttribute("aria-controls", id);
    input.setAttribute("aria-expanded", "false");
    const name = select.getAttribute("aria-label") || select.closest("label")?.querySelector("span")?.textContent.trim();
    if (name) input.setAttribute("aria-label", name);
    const list = document.createElement("ul");
    list.id = id;
    list.setAttribute("role", "listbox");
    list.hidden = true;
    list.className = "rounded-md border border-slate-300 bg-white text-sm shadow-sm";
    list.style.cssText = "position:absolute;left:0;right:0;z-index:20;max-height:16rem;overflow:auto;margin:2px 0 0;padding:0;list-style:none";
    wrap.append(input, list);
    select.before(wrap);
    select.hidden = true;
    // A hidden required select blocks submit without a message; the visible input carries it.
    input.required = select.required;
    select.required = false;
    const chosen = () => entries.find((e) => e.value === select.value);
    const sync = () => {
      input.value = chosen()?.label ?? "";
    };
    sync();
    list.addEventListener("mousedown", (ev) => ev.preventDefault());

    let shown = [];
    let active = -1;
    const open = (on) => {
      list.hidden = !on;
      input.setAttribute("aria-expanded", String(on));
      if (!on) input.removeAttribute("aria-activedescendant");
    };
    const mark = (i) => {
      active = i;
      list.querySelectorAll("[role=option]").forEach((li, j) => {
        li.setAttribute("aria-selected", String(j === i));
        li.style.background = j === i ? "#e2e8f0" : "";
      });
      const li = list.children[i];
      if (li) {
        input.setAttribute("aria-activedescendant", li.id);
        li.scrollIntoView?.({ block: "nearest" });
      } else {
        input.removeAttribute("aria-activedescendant");
      }
    };
    const render = () => {
      shown = global.teamsterFilterPrincipals(entries, input.value);
      list.replaceChildren(
        ...shown.map((e, i) => {
          const li = document.createElement("li");
          li.id = id + "-" + i;
          li.setAttribute("role", "option");
          li.title = e.title;
          li.style.cssText = "display:flex;gap:.5rem;align-items:center;padding:.375rem .75rem;cursor:pointer";
          const badge = document.createElement("span");
          badge.className = "rounded bg-slate-100 px-1.5 py-0.5 text-xs text-slate-600";
          badge.textContent = badges[e.kind];
          li.append(badge, document.createTextNode(e.label));
          // mousedown, so the pick lands before the input's blur closes the list.
          li.addEventListener("mousedown", (ev) => {
            ev.preventDefault();
            pick(e);
          });
          return li;
        }),
      );
      open(shown.length > 0);
      mark(-1);
    };
    const pick = (e) => {
      select.value = e.value;
      input.value = e.label;
      open(false);
      select.dispatchEvent(new Event("change", { bubbles: true }));
    };

    // A page lists the first users only; past them, typing asks the server.
    const searchURL = select.dataset.searchUrl;
    let searching = null;
    let pending = null;
    const search = () => {
      const q = input.value.trim();
      if (!searchURL || q.length < 2) return;
      window.clearTimeout(pending);
      pending = window.setTimeout(async () => {
        searching?.abort();
        searching = new AbortController();
        try {
          const res = await fetch(searchURL + "?q=" + encodeURIComponent(q), {
            headers: { Accept: "application/json" },
            signal: searching.signal,
          });
          if (!res.ok) return;
          const added = global.teamsterNewPrincipals(entries, await res.json());
          if (!added.length) return;
          const group = select.querySelector('option[value^="user:"]')?.parentElement || select;
          added.forEach((e) => {
            const option = new Option(e.label, e.value);
            option.dataset.search = e.search;
            option.title = e.title;
            group.append(option);
            entries.push(e);
          });
          badges.user = badges.user || (group.label ?? "user");
          if (document.activeElement === input && input.value.trim() === q) render();
        } catch {
          // An aborted or failed search leaves the listed users.
        }
      }, 250);
    };

    input.addEventListener("input", () => {
      if (!input.value) select.value = "";
      render();
      search();
    });
    input.addEventListener("focus", render);
    input.addEventListener("blur", () => {
      open(false);
      sync();
    });
    input.addEventListener("keydown", (ev) => {
      if (ev.key === "ArrowDown" || ev.key === "ArrowUp") {
        ev.preventDefault();
        if (list.hidden) render();
        if (!shown.length) return;
        mark(global.teamsterStepPrincipal(active, ev.key === "ArrowDown" ? 1 : -1, shown.length));
      } else if (ev.key === "Enter") {
        if (!list.hidden) ev.preventDefault();
        if (active >= 0) pick(shown[active]);
      } else if (ev.key === "Escape") {
        open(false);
        sync();
      }
    });
  });
})(typeof window !== "undefined" ? window : globalThis);
