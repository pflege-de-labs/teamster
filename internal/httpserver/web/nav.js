// Adds the burger that folds the sidebar away, remembering the choice, marks the
// current page in it, closes the user menu on a click outside or Escape, and
// shows a progress bar while a slow page loads.
(function (global) {
  // leaves tells whether a click or submit on el makes this tab load another page.
  function leaves(event, el, here) {
    if (el.tagName === "FORM") {
      return el.method !== "dialog" && (!el.target || el.target === "_self");
    }
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return false;
    if ((el.target && el.target !== "_self") || el.hasAttribute("download")) return false;
    if (el.origin !== here.origin) return false;
    // A jump within the page loads nothing.
    return !(el.hash && el.pathname === here.pathname && el.search === here.search);
  }
  global.teamsterNavLeaves = leaves;
  if (typeof document === "undefined") return;

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

    for (const link of sidebar.querySelectorAll("a[href]")) {
      if (link.pathname === location.pathname) link.setAttribute("aria-current", "page");
    }
  }

  const bar = document.getElementById("nav-progress");
  if (bar) {
    let timer;
    const pending = (event, el) => {
      if (!el || !leaves(event, el, location)) return;
      clearTimeout(timer);
      // Checked late so a script that handles the click itself has had its say;
      // the delay keeps fast pages from flashing the bar.
      timer = setTimeout(() => {
        if (!event.defaultPrevented) bar.hidden = false;
      }, 150);
    };
    document.addEventListener("click", (event) => pending(event, event.target.closest?.("a[href]")));
    document.addEventListener("submit", (event) => pending(event, event.target));
    // Back and forward restore the page from cache with the bar still showing.
    window.addEventListener("pageshow", () => {
      clearTimeout(timer);
      bar.hidden = true;
    });
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
})(typeof window !== "undefined" ? window : globalThis);
