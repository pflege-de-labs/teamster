// Fills in the browser's time zone, which only a script can read, and shows the
// button that sends it.
(function () {
  const form = document.getElementById("timezone-switch");
  if (!form) return;

  let zone = "";
  try {
    zone = Intl.DateTimeFormat().resolvedOptions().timeZone || "";
  } catch (e) {}
  if (!zone) return;

  form.elements.zone.value = zone;
  const browser = form.querySelector('button[value="browser"]');
  if (browser) browser.hidden = false;
})();
