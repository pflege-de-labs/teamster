// Adds the burger that folds the sidebar away, remembering the choice, and closes the user menu on a click outside or Escape.
(function (global) {
  // Cards sent by v0.6-v0.8 link /admin#<section>, and a fragment never reaches the server's redirect.
  global.teamsterNavLegacyTarget = (pathname, hash) =>
    pathname === "/admin/routing" && /^#(templates|destinations|webhooks|routes|grants)$/.test(hash)
      ? "/admin/" + hash.slice(1)
      : null;
  if (typeof document === "undefined") return;
  const legacy = global.teamsterNavLegacyTarget(location.pathname, location.hash);
  if (legacy) {
    location.replace(legacy);
    return;
  }

  const toggle = document.getElementById("nav-toggle");
  const sidebar = document.getElementById("sidebar");
  if (toggle && sidebar) {
    const key = "teamster.sidebar";
    const root = document.documentElement;
    // The head script already applied the stored state; this only keeps aria in step.
    const show = (open) => {
      root.classList.toggle("sidebar-closed", !open);
      toggle.setAttribute("aria-expanded", String(open));
    };
    show(!root.classList.contains("sidebar-closed"));
    toggle.addEventListener("click", () => {
      const open = root.classList.contains("sidebar-closed");
      show(open);
      try {
        localStorage.setItem(key, open ? "open" : "closed");
      } catch (e) {
        // Not remembered, but still toggled.
      }
    });
  }

  document.querySelectorAll("#sidebar details[data-group]").forEach((group) => {
    // Click, not toggle: the parser-opened current group would fire toggle and drop its stored state.
    group.querySelector("summary").addEventListener("click", () => {
      try {
        const key = "teamster.nav." + group.dataset.group;
        // group.open is still the pre-click value here.
        if (group.open) localStorage.setItem(key, "closed");
        else localStorage.removeItem(key);
      } catch (e) {
        // Not remembered, but still toggled.
      }
    });
  });

  const menu = document.getElementById("user-menu");
  if (menu) {
    document.addEventListener("click", (event) => {
      if (menu.open && !menu.contains(event.target)) menu.open = false;
    });
    document.addEventListener("keydown", (event) => {
      if (event.key === "Escape" && menu.open) {
        menu.open = false;
        menu.querySelector("summary").focus();
      }
    });
  }
})(typeof window !== "undefined" ? window : globalThis);
