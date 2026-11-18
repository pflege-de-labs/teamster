// Offers only the actions the chosen resource has; the server answers a wrong pair anyway.
(function () {
  const form = document.querySelector("[data-access-check]");
  if (!form) return;
  const resource = form.elements.resource;
  const action = form.elements.action;

  function sync() {
    const allowed = (resource.selectedOptions[0]?.dataset.actions || "").split(" ");
    for (const option of action.options) {
      option.hidden = option.disabled = !allowed.includes(option.value);
    }
    if (action.selectedOptions[0]?.disabled) {
      action.value = Array.from(action.options).find((o) => !o.disabled)?.value ?? "";
    }
  }

  resource.addEventListener("change", sync);
  sync();
})();
