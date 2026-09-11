// Renders a template against a sample alert. The template is Go text/template,
// so the server does the templating and the browser only draws the result.
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

  // Inserting at the cursor rather than appending: an operator adding a fact
  // to a card wants it where they are looking, not at the end of the file.
  function insertAtCursor(field, text) {
    const start = field.selectionStart === null ? field.value.length : field.selectionStart;
    const end = field.selectionEnd === null ? start : field.selectionEnd;

    // A fragment dropped between two array entries needs the comma that the
    // person would otherwise have to remember; one dropped into an empty field
    // does not.
    const before = field.value.slice(0, start);
    const after = field.value.slice(end);
    const needsComma = /[}\]"]\s*$/.test(before) && !/^\s*[,\]}]/.test(after);
    const fragment = (needsComma ? ",\n" : "") + text;

    field.value = before + fragment + after;
    const caret = start + fragment.length;
    field.setSelectionRange(caret, caret);
    field.focus();
    // Typing is what the live preview listens for, and this is typing.
    field.dispatchEvent(new Event("input", { bubbles: true }));
  }

  function valueOf(name) {
    const field = form.querySelector('[name="' + name + '"]');
    return field ? field.value : "";
  }

  // The feed line is the reason a title exists, so the preview leads with it.
  function feedLine(title) {
    const wrapper = document.createElement("div");
    wrapper.className = "rounded-md border border-slate-200 bg-slate-50 px-4 py-3";

    const caption = document.createElement("p");
    caption.className = "text-xs font-medium uppercase tracking-wide text-slate-500";
    caption.textContent = "Teams activity feed";
    wrapper.appendChild(caption);

    const line = document.createElement("p");
    line.className = "mt-1 truncate text-sm font-semibold text-slate-900";
    line.textContent = title;
    wrapper.appendChild(line);
    return wrapper;
  }

  // The server sanitizes the text to an allowlist; the sandboxed frame is the
  // second lock, so a gap in the first one still cannot run anything here.
  function textFrame(text) {
    const frame = document.createElement("iframe");
    frame.setAttribute("sandbox", "");
    frame.setAttribute("title", "Message text");
    frame.className = "mt-2 h-32 w-full rounded-md border border-slate-200 bg-white";
    frame.srcdoc =
      '<!doctype html><meta charset="utf-8"><body style="font: 14px system-ui, sans-serif; margin: 12px">' + text;
    return frame;
  }

  const bodyField = form.querySelector('[name="body"]');

  form.querySelectorAll("button.snippet").forEach((snippet) => {
    snippet.addEventListener("click", () => {
      if (bodyField) insertAtCursor(bodyField, snippet.dataset.snippet || "");
    });
  });

  const starter = document.getElementById("card-starter");
  if (starter && bodyField) {
    starter.addEventListener("click", () => {
      // Replacing what is there is the point of starting again, but not
      // silently: a card someone has been working on is worth a question.
      if (bodyField.value.trim() !== "" && !window.confirm("Replace the card with the example?")) {
        return;
      }
      bodyField.value = starter.dataset.snippet || "";
      bodyField.dispatchEvent(new Event("input", { bubbles: true }));
      bodyField.focus();
    });
  }

  // Previewing as you type, debounced: the render is a round trip to the
  // server, and one per keystroke would be a request per keystroke.
  const live = document.getElementById("preview-live");
  let pending = null;
  function schedulePreview() {
    if (!live || !live.checked) return;
    window.clearTimeout(pending);
    pending = window.setTimeout(() => button.click(), 600);
  }

  ["title", "message_text", "body"].forEach((name) => {
    const field = form.querySelector('[name="' + name + '"]');
    if (field) field.addEventListener("input", schedulePreview);
  });
  if (sample) sample.addEventListener("change", schedulePreview);

  button.addEventListener("click", async () => {
    let payload;
    try {
      const res = await fetch("/api/templates/preview", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          title: valueOf("title"),
          text: valueOf("message_text"),
          body: valueOf("body"),
          sample: sample ? sample.value : "firing",
        }),
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

    const parts = [];
    if (payload.title) parts.push(feedLine(payload.title));
    if (payload.text) parts.push(textFrame(payload.text));

    if (payload.card) {
      try {
        const card = new AdaptiveCards.AdaptiveCard();
        card.parse(payload.card);
        parts.push(card.render());
      } catch (err) {
        report("The card rendered to JSON but the renderer rejected it: " + err.message);
        return;
      }
    }

    if (parts.length === 0) {
      report("This template renders to nothing. Give it a title, text or a card.");
      return;
    }
    output.replaceChildren(...parts);
  });
})();
