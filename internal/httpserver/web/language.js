// Submits the language picker as soon as a choice is made, and hides the button
// that exists for browsers that will not run this.
(function () {
  const form = document.getElementById("language-picker");
  if (!form) return;

  const select = document.getElementById("language-select");
  const apply = document.getElementById("language-apply");
  if (!select) return;

  if (apply) apply.hidden = true;
  select.addEventListener("change", () => form.submit());
})();
