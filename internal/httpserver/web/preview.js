// Renders a template against a sample alert. The template is Go text/template,
// so the server does the templating and the browser only draws the card.
(function () {
  const form = document.getElementById("template-form");
  const button = document.getElementById("preview-button");
  const sample = document.getElementById("preview-sample");
  const output = document.getElementById("preview-output");
  if (!form || !button || !output) return;

  function report(message) {
    output.replaceChildren();
    const p = document.createElement("p");
    p.className = "rounded-md border border-red-300 bg-red-50 px-4 py-3 text-sm text-red-800";
    p.textContent = message;
    output.appendChild(p);
  }

  button.addEventListener("click", async () => {
    const field = form.querySelector('[name="body"]');
    const body = field ? field.value : "";

    let payload;
    try {
      const res = await fetch("/api/templates/preview", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ body: body, sample: sample ? sample.value : "firing" }),
      });
      payload = await res.json();
    } catch (err) {
      report("Preview request failed: " + err.message);
      return;
    }

    if (payload.error) {
      report(payload.error);
      return;
    }

    try {
      const card = new AdaptiveCards.AdaptiveCard();
      card.parse(payload.card);
      const rendered = card.render();
      output.replaceChildren(rendered);
    } catch (err) {
      report("The card rendered to JSON but the renderer rejected it: " + err.message);
    }
  });
})();
