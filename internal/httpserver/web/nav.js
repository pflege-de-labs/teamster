// Adds the burger that folds the sidebar away, remembering the choice, marks the
// current page in it, and closes the user menu on a click outside or Escape.
(function () {
  const toggle = document.getElementById("nav-toggle");
  const sidebar = document.getElementById("sidebar");
  if (toggle && sidebar) {
    const key = "teamster.sidebar";
    const show = (open) => {
      sidebar.hidden = !open;
      toggle.setAttribute("aria-expanded", String(open));
    };
    let stored = null;
    try {
      stored = localStorage.getItem(key);
    } catch (e) {
      // Storage may be disabled; the sidebar then starts open every time.
    }
    show(stored !== "closed");
    toggle.hidden = false;
    toggle.addEventListener("click", () => {
      const open = sidebar.hidden;
      show(open);
      try {
        localStorage.setItem(key, open ? "open" : "closed");
      } catch (e) {
        // Not remembered, but still toggled.
      }
    });

    for (const link of sidebar.querySelectorAll("a[href]")) {
      if (link.pathname === location.pathname) link.setAttribute("aria-current", "page");
    }
  }

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
})();
