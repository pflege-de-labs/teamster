// Switches the /admin tabs in place. The server already rendered the right one,
// so this only saves the round trip and keeps the URL naming the open tab.
(function () {
  const bar = document.querySelector("[data-admin-tabs]");
  if (!bar) return;

  const tabs = Array.from(bar.querySelectorAll("[data-tab]"));
  const panels = Array.from(document.querySelectorAll("[data-tab-panel]"));

  function show(name) {
    if (!tabs.some((tab) => tab.dataset.tab === name)) return false;
    for (const tab of tabs) tab.setAttribute("aria-selected", String(tab.dataset.tab === name));
    for (const panel of panels) panel.hidden = panel.dataset.tabPanel !== name;
    return true;
  }

  function remember(name) {
    const url = new URL(location.href);
    url.searchParams.set("tab", name);
    // A notice belongs to the post that raised it, not to the next tab.
    url.searchParams.delete("notice");
    url.searchParams.delete("error");
    history.replaceState(null, "", url.pathname + url.search);
  }

  for (const tab of tabs) {
    tab.addEventListener("click", (event) => {
      if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return;
      event.preventDefault();
      if (show(tab.dataset.tab)) remember(tab.dataset.tab);
    });
  }

  // Links written before the tabs, such as /admin#templates in cards already sent.
  function fromHash() {
    const section = location.hash.slice(1);
    const panel = section && document.getElementById(section)?.closest("[data-tab-panel]");
    if (panel && show(panel.dataset.tabPanel)) remember(panel.dataset.tabPanel);
  }
  fromHash();
  window.addEventListener("hashchange", fromHash);
})();
