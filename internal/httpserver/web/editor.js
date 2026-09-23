// Upgrades the fields marked data-editor to CodeMirror editors with completion
// (ADR 0041). The field stays in the form and is kept in step with every edit,
// so posting, preview.js and routing.js read it exactly as before; without
// JavaScript, or if anything here fails, the plain field is what remains.
//
// Modes: "template" and "template-line" are Go template text, "card" is a
// Go-templated Adaptive Card, "selector" is a route's JSON label selector and
// "match" is the routing page's key=value lines.

const fields = document.querySelectorAll("[data-editor]");
if (fields.length > 0) {
  load().catch((err) => console.warn("editor: staying with plain fields", err));
}

async function load() {
  const [state, view, language, autocomplete, commands, json, lezer, highlight] = await Promise.all([
    import("@codemirror/state"),
    import("@codemirror/view"),
    import("@codemirror/language"),
    import("@codemirror/autocomplete"),
    import("@codemirror/commands"),
    import("@codemirror/lang-json"),
    import("@lezer/common"),
    import("@lezer/highlight"),
  ]);
  const cm = { state, view, language, autocomplete, commands, json, lezer, highlight };
  const ctx = {
    cm,
    samples: loadSamples(),
    vocabulary: readJSON("template-vocabulary") || { fields: [], labelMaps: [], annotationMaps: [], functions: [], keywords: [] },
    card: cardSchema(),
    template: goTemplate(cm),
  };
  fields.forEach((field) => {
    try {
      upgrade(field, ctx);
    } catch (err) {
      console.warn("editor: left a field as it was", field.name, err);
    }
  });
}

// Fetched once per page; only editors are sent the endpoint, see layout.templ.
function loadSamples() {
  const meta = document.querySelector('meta[name="teamster-samples"]');
  const empty = { labels: {}, annotations: [] };
  if (!meta) return Promise.resolve(empty);
  return fetch(meta.content, { headers: { Accept: "application/json" } })
    .then((res) => (res.ok ? res.json() : empty))
    .then((body) => ({
      labels: body && typeof body.labels === "object" && body.labels ? body.labels : {},
      annotations: body && Array.isArray(body.annotations) ? body.annotations : [],
    }))
    .catch(() => empty);
}

function readJSON(id) {
  const node = document.getElementById(id);
  if (!node) return null;
  try {
    return JSON.parse(node.textContent);
  } catch (err) {
    return null;
  }
}

function upgrade(field, ctx) {
  const { EditorState, EditorSelection } = ctx.cm.state;
  const { EditorView, keymap, drawSelection, placeholder } = ctx.cm.view;
  const { syntaxHighlighting, defaultHighlightStyle, bracketMatching } = ctx.cm.language;
  const { autocompletion, completionKeymap, closeBrackets, closeBracketsKeymap } = ctx.cm.autocomplete;
  const { defaultKeymap, history, historyKeymap } = ctx.cm.commands;

  const mode = field.dataset.editor;
  const singleLine = field.tagName === "INPUT";
  const rows = Number(field.getAttribute("rows")) || 1;
  const label = field.closest("label");
  const labelText = label ? (label.querySelector("span") || label).textContent.trim() : field.name;

  let syncing = false;
  const extensions = [
    history(),
    drawSelection(),
    bracketMatching(),
    closeBrackets(),
    syntaxHighlighting(ctx.template.style),
    syntaxHighlighting(defaultHighlightStyle),
    keymap.of([...closeBracketsKeymap, ...completionKeymap, ...historyKeymap, ...defaultKeymap]),
    autocompletion({ override: sourcesFor(mode, ctx) }),
    EditorView.contentAttributes.of({ "aria-label": labelText }),
    // Through the facet: the view rewrites its own class attribute.
    EditorView.editorAttributes.of({ class: field.className }),
    EditorView.theme({
      "&": { backgroundColor: "#fff" },
      "&.cm-focused": { outline: "none", borderColor: "#0ea5e9" },
      ".cm-scroller": { fontFamily: "inherit", lineHeight: "1.5" },
      ".cm-content": { minHeight: singleLine ? "auto" : rows * 1.5 + "em", padding: "0" },
    }),
    EditorView.updateListener.of((update) => {
      if (!update.docChanged) return;
      field.value = update.state.doc.toString();
      // Typing is what preview.js listens for, and this is typing.
      if (!syncing) field.dispatchEvent(new Event("input", { bubbles: true }));
    }),
  ];
  if (!singleLine) extensions.push(EditorView.lineWrapping);
  if (field.placeholder) extensions.push(placeholder(field.placeholder));
  if (singleLine) {
    // A title is one line; a pasted newline would only be folded away on render.
    extensions.push(EditorState.transactionFilter.of((tr) => (tr.newDoc.lines > 1 ? [] : tr)));
  }
  if (mode === "card") extensions.push(ctx.template.card);
  else if (mode === "template" || mode === "template-line") extensions.push(ctx.template.text);
  else if (mode === "selector") extensions.push(ctx.cm.json.json());

  const editor = new EditorView({
    state: EditorState.create({ doc: field.value, extensions }),
  });
  (label || field).after(editor.dom);
  field.hidden = true;

  // Someone else set the field, as the card starter does: follow it.
  field.addEventListener("input", () => {
    const current = editor.state.doc.toString();
    if (field.value === current) return;
    syncing = true;
    editor.dispatch({ changes: { from: 0, to: current.length, insert: field.value } });
    syncing = false;
    editor.focus();
  });

  // The card palette, see preview.js.
  field.addEventListener("teamster:insert", (event) => {
    if (!event.detail || typeof event.detail.fragment !== "function") return;
    event.preventDefault();
    const range = editor.state.selection.main;
    const doc = editor.state.doc;
    const fragment = event.detail.fragment(doc.sliceString(0, range.from), doc.sliceString(range.to));
    editor.dispatch({
      changes: { from: range.from, to: range.to, insert: fragment },
      selection: EditorSelection.cursor(range.from + fragment.length),
      scrollIntoView: true,
      userEvent: "input.paste",
    });
    editor.focus();
  });
}

function sourcesFor(mode, ctx) {
  switch (mode) {
    case "template":
    case "template-line":
      return [templateSource(ctx)];
    case "card":
      return [templateSource(ctx), cardSource(ctx)];
    case "selector":
      return [selectorSource(ctx)];
    case "match":
      return [matchSource(ctx)];
    default:
      return [];
  }
}

// ---- Go template syntax -------------------------------------------------

// A hand-written parser rather than a Lezer grammar, because a grammar needs a
// build step and this repository has none. It splits the text into Text and
// Action nodes and tokenizes inside each action; parseMixed then lays the
// JSON parser over the Text nodes alone, so an action is never JSON to it.
function goTemplate(cm) {
  const { NodeType, NodeSet, Tree, Parser, parseMixed } = cm.lezer;
  const { Language, defineLanguageFacet, languageDataProp, HighlightStyle } = cm.language;
  const { styleTags, tags, Tag } = cm.highlight;
  const { jsonLanguage } = cm.json;

  const names = ["Document", "Text", "Action", "Delim", "Keyword", "Field", "Variable", "String", "Number", "Function", "Comment", "Punct"];
  const id = Object.fromEntries(names.map((name, i) => [name, i]));
  const actionTag = Tag.define();
  const data = defineLanguageFacet({ commentTokens: { block: { open: "{{/*", close: "*/}}" } } });
  const nodeSet = new NodeSet(
    names.map((name, i) => NodeType.define({ id: i, name, top: i === 0, props: i === 0 ? [[languageDataProp, data]] : [] })),
  ).extend(
    styleTags({
      Action: actionTag,
      Delim: tags.brace,
      Keyword: tags.controlKeyword,
      Field: tags.propertyName,
      Variable: tags.variableName,
      String: tags.string,
      Number: tags.number,
      Function: tags.function(tags.variableName),
      Comment: tags.blockComment,
      Punct: tags.operator,
    }),
  );
  const keywords = new Set(["if", "else", "end", "range", "with", "define", "template", "block", "break", "continue", "nil", "true", "false"]);
  const token = /\s+|\/\*[\s\S]*?\*\/|"(?:[^"\\]|\\.)*"?|`[^`]*`?|'(?:[^'\\]|\\.)*'?|\$[\w]*(?:\.\w+)*|(?:\.\w*)+|-?\d[\w.]*|[A-Za-z_]\w*(?:\.\w+)*|:=|[|(),=]|./y;

  function tokens(body, offset, out) {
    token.lastIndex = 0;
    let match;
    while (token.lastIndex < body.length && (match = token.exec(body))) {
      const text = match[0];
      const from = offset + match.index;
      let type = null;
      if (/^\s/.test(text)) type = null;
      else if (text.startsWith("/*")) type = id.Comment;
      else if (/^["`']/.test(text)) type = id.String;
      else if (text.startsWith("$")) type = id.Variable;
      else if (text.startsWith(".")) type = id.Field;
      else if (/^-?\d/.test(text)) type = id.Number;
      else if (/^[A-Za-z_]/.test(text)) type = keywords.has(text) ? id.Keyword : id.Function;
      else type = id.Punct;
      if (type !== null) out.push(type, from, from + text.length, 4);
    }
  }

  function build(text, start) {
    const buffer = [];
    let pos = 0;
    while (pos < text.length) {
      const open = text.indexOf("{{", pos);
      if (open < 0) {
        buffer.push(id.Text, start + pos, start + text.length, 4);
        break;
      }
      if (open > pos) buffer.push(id.Text, start + pos, start + open, 4);
      const close = text.indexOf("}}", open + 2);
      const end = close < 0 ? text.length : close + 2;
      const trimLeft = text.startsWith("{{- ", open) ? 3 : 2;
      const trimRight = close >= 0 && text.slice(close - 2, close) === " -" ? 2 : 0;
      const inner = [id.Delim, start + open, start + open + trimLeft, 4];
      const bodyEnd = close < 0 ? text.length : close - trimRight;
      tokens(text.slice(open + trimLeft, bodyEnd), start + open + trimLeft, inner);
      if (close >= 0) inner.push(id.Delim, start + bodyEnd, start + end, 4);
      buffer.push(...inner, id.Action, start + open, start + end, inner.length + 4);
      pos = end;
    }
    return Tree.build({ buffer, nodeSet, topID: id.Document, start, length: text.length });
  }

  class TemplateParser extends Parser {
    constructor(wrapper) {
      super();
      this.wrapper = wrapper;
    }
    createParse(input, fragments, ranges) {
      const from = ranges[0].from;
      const to = ranges[ranges.length - 1].to;
      const parse = {
        parsedPos: from,
        stoppedAt: null,
        stopAt(pos) {
          this.stoppedAt = pos;
        },
        advance() {
          this.parsedPos = to;
          return build(input.read(from, to), from);
        },
      };
      return this.wrapper ? this.wrapper(parse, input, fragments, ranges) : parse;
    }
  }

  const overlay = parseMixed((node) =>
    node.type.isTop ? { parser: jsonLanguage.parser, overlay: (child) => child.type.name === "Text" } : null,
  );

  return {
    text: new Language(data, new TemplateParser(null), [], "gotemplate").extension,
    card: new Language(data, new TemplateParser(overlay), [], "gotemplate-json").extension,
    style: HighlightStyle.define([
      { tag: actionTag, backgroundColor: "#eef2ff", borderRadius: "2px" },
      { tag: tags.brace, color: "#4f46e5", fontWeight: "600" },
      { tag: tags.controlKeyword, color: "#7c3aed", fontWeight: "600" },
      { tag: tags.function(tags.variableName), color: "#0369a1" },
      { tag: tags.variableName, color: "#b45309" },
      { tag: tags.blockComment, color: "#64748b", fontStyle: "italic" },
    ]),
  };
}

// The text between the last {{ before pos and pos, or null outside an action.
function actionBefore(state, pos) {
  const text = state.doc.sliceString(Math.max(0, pos - 4000), pos);
  const open = text.lastIndexOf("{{");
  if (open < 0 || text.indexOf("}}", open) >= 0) return null;
  return text.slice(open + 2).replace(/^-\s/, " ");
}

const identifier = /^[A-Za-z_]\w*$/;

function templateSource(ctx) {
  const vocab = ctx.vocabulary;
  const words = [
    ...vocab.functions.map((label) => ({ label, type: "function" })),
    ...vocab.keywords.map((label) => ({ label, type: "keyword" })),
  ];

  return async (context) => {
    const inside = actionBefore(context.state, context.pos);
    if (inside === null || inside.trimStart().startsWith("/*")) return null;
    const samples = await ctx.samples;

    // index .Alert.Labels "se|
    const indexed = /\bindex\s+(\.[\w.]+)\s+"([^"]*)$/.exec(inside);
    if (indexed) {
      const keys = keysOf(indexed[1], vocab, samples);
      if (!keys) return null;
      return { from: context.pos - indexed[2].length, options: keys.map((label) => ({ label, type: "property" })), validFor: /^[^"]*$/ };
    }
    // Anywhere else inside a string there is nothing to offer.
    if ((inside.match(/"/g) || []).length % 2 === 1) return null;

    const word = /[$\w.]*$/.exec(inside)[0];
    if (word.startsWith("$")) return null;
    const dot = word.lastIndexOf(".");
    if (dot < 0) {
      if (!word && !context.explicit) return null;
      return { from: context.pos - word.length, options: words, validFor: /^\w*$/ };
    }

    const base = word.slice(0, dot);
    const partial = word.slice(dot + 1);
    const from = context.pos - partial.length;
    const keys = keysOf(base, vocab, samples);
    if (keys) {
      const start = context.pos - word.length;
      return {
        from,
        options: keys.map((key) =>
          identifier.test(key)
            ? { label: key, type: "property" }
            : // A key like app.kubernetes.io/name cannot follow a dot.
              { label: key, type: "property", apply: replaceFrom(start, "(index " + base + " " + JSON.stringify(key) + ")") },
        ),
        validFor: /^[\w./-]*$/,
      };
    }

    const children = vocab.fields
      .filter((field) => field.slice(0, field.lastIndexOf(".")) === base)
      .map((field) => ({ label: field.slice(field.lastIndexOf(".") + 1), type: "variable" }));
    if (children.length === 0) return null;
    return { from, options: children, validFor: /^\w*$/ };
  };
}

function keysOf(base, vocab, samples) {
  if (vocab.labelMaps.includes(base)) return Object.keys(samples.labels).sort();
  if (vocab.annotationMaps.includes(base)) return samples.annotations;
  return null;
}

function replaceFrom(start, text) {
  return (view, completion, from, to) => {
    view.dispatch({ changes: { from: start, to, insert: text }, selection: { anchor: start + text.length } });
  };
}

// ---- Adaptive Cards -----------------------------------------------------

// Read out of the renderer the preview already loads, so completion offers what
// that renderer understands and follows it when it is bumped. A few properties
// are parsed by hand in the library rather than declared in its schema, and a
// few objects have an implied type; those two lists are the only ones kept here.
function cardSchema() {
  const AC = window.AdaptiveCards;
  const schema = { elements: [], actions: [], props: {}, enums: {} };
  if (!AC || !AC.GlobalRegistry) return schema;

  const describe = (name, instance) => {
    try {
      const s = instance.getSchema();
      const props = [];
      const enums = {};
      for (let i = 0; i < s.getCount(); i++) {
        const p = s.getItemAt(i);
        if (!p || typeof p.name !== "string" || p.name.includes(".")) continue;
        props.push(p.name);
        if (Array.isArray(p.values)) {
          const values = p.values
            .map((v) => (typeof v.value === "string" ? v.value : p.enumType && p.enumType[v.value]))
            .filter((v) => typeof v === "string")
            .map((v) => v.charAt(0).toLowerCase() + v.slice(1));
          if (values.length) enums[p.name] = values;
        }
      }
      schema.props[name] = props.concat(handParsed[name] || []);
      schema.enums[name] = enums;
    } catch (err) {
      // An object the library cannot construct on its own is simply not offered.
    }
  };

  const registries = [
    [AC.GlobalRegistry.elements, schema.elements],
    [AC.GlobalRegistry.actions, schema.actions],
  ];
  for (const [registry, names] of registries) {
    for (let i = 0; i < registry.getItemCount(); i++) {
      const item = registry.getItemAt(i);
      names.push(item.typeName);
      try {
        describe(item.typeName, new item.objectType());
      } catch (err) {
        // As above.
      }
    }
  }
  describe("AdaptiveCard", new AC.AdaptiveCard());
  for (const name of Object.values(implied)) {
    if (!schema.props[name] && typeof AC[name] === "function") describe(name, new AC[name]());
  }
  return schema;
}

const handParsed = {
  AdaptiveCard: ["body", "actions"],
  Container: ["items", "fallback"],
  Column: ["items"],
  ColumnSet: ["columns"],
  ActionSet: ["actions"],
  "Action.ShowCard": ["card"],
  Table: ["rows"],
  TableRow: ["cells"],
  TableCell: ["items"],
  RichTextBlock: ["inlines"],
  Carousel: ["pages"],
};

const implied = {
  facts: "Fact",
  choices: "Choice",
  sources: "MediaSource",
  rows: "TableRow",
  cells: "TableCell",
  inlines: "TextRun",
  backgroundImage: "BackgroundImage",
  pages: "CarouselPage",
};

function cardSource(ctx) {
  const { syntaxTree } = ctx.cm.language;
  const schema = ctx.card;
  const allProps = [...new Set(Object.values(schema.props).flat())].sort();

  return (context) => {
    if (actionBefore(context.state, context.pos) !== null) return null;
    const before = context.state.doc.sliceString(Math.max(0, context.pos - 500), context.pos);
    const object = objectAt(syntaxTree(context.state), context.state, context.pos);

    let m = /"type"\s*:\s*"([\w.]*)$/.exec(before);
    if (m) {
      const actions = object && object.parent === "actions";
      const options = [
        ...schema.elements.map((label) => ({ label, type: "class", boost: actions ? -1 : 1 })),
        ...schema.actions.map((label) => ({ label, type: "class", boost: actions ? 1 : -1 })),
      ];
      return { from: context.pos - m[1].length, options, validFor: /^[\w.]*$/ };
    }
    m = /"(\w+)"\s*:\s*"(\w*)$/.exec(before);
    if (m) {
      const values = object && schema.enums[object.kind] && schema.enums[object.kind][m[1]];
      if (!values) return null;
      return { from: context.pos - m[2].length, options: values.map((label) => ({ label, type: "enum" })), validFor: /^\w*$/ };
    }
    m = /[{,]\s*"([\w$]*)$/.exec(before);
    if (m) {
      const props = (object && schema.props[object.kind]) || allProps;
      return { from: context.pos - m[1].length, options: props.map(propertyOption), validFor: /^[\w$]*$/ };
    }
    return null;
  };
}

// Completes "name": and steps over the quote closeBrackets already typed.
function propertyOption(label) {
  return {
    label,
    type: "property",
    apply: (view, completion, from, to) => {
      const closed = view.state.sliceDoc(to, to + 1) === '"';
      const insert = label + '": ';
      view.dispatch({ changes: { from, to: closed ? to + 1 : to, insert }, selection: { anchor: from + insert.length } });
    },
  };
}

// What kind of card object the cursor is in, and the property holding it.
function objectAt(tree, state, pos) {
  let node = tree.resolveInner(pos, -1);
  while (node && node.name !== "Object") node = node.parent;
  if (!node) return null;

  const props = propertiesOf(state, node);
  let parent = null;
  let owner = null;
  let holder = node.parent;
  if (holder && holder.name === "Array") holder = holder.parent;
  if (holder && holder.name === "Property") {
    parent = propertyName(state, holder);
    let outer = holder.parent;
    while (outer && outer.name !== "Object") outer = outer.parent;
    if (outer) owner = propertiesOf(state, outer).type || null;
  }

  let kind = props.type;
  if (!kind && !parent) kind = "AdaptiveCard";
  else if (!kind && parent === "columns") kind = owner === "Table" ? "TableColumnDefinition" : "Column";
  else if (!kind) kind = implied[parent];
  return { kind, parent };
}

function propertiesOf(state, object) {
  const out = {};
  for (let child = object.firstChild; child; child = child.nextSibling) {
    if (child.name !== "Property") continue;
    const name = propertyName(state, child);
    const value = child.lastChild;
    if (name && value && value.name === "String") out[name] = unquote(state.sliceDoc(value.from, value.to));
  }
  return out;
}

function propertyName(state, property) {
  const name = property.getChild("PropertyName");
  return name ? unquote(state.sliceDoc(name.from, name.to)) : null;
}

function unquote(text) {
  try {
    return JSON.parse(text);
  } catch (err) {
    return text.replace(/^"|"$/g, "");
  }
}

// ---- Route selectors and the routing page ----------------------------------

function selectorSource(ctx) {
  const { startCompletion } = ctx.cm.autocomplete;
  return async (context) => {
    const before = context.state.doc.sliceString(Math.max(0, context.pos - 500), context.pos);
    const samples = await ctx.samples;

    let m = /"((?:[^"\\]|\\.)*)"\s*:\s*"((?:[^"\\]|\\.)*)$/.exec(before);
    if (m) {
      const values = samples.labels[unquote('"' + m[1] + '"')] || [];
      return {
        from: context.pos - m[2].length,
        options: values.map((value, i) => ({ label: JSON.stringify(value).slice(1, -1), type: "text", boost: -Math.min(i, 99) })),
        validFor: /^[^"]*$/,
      };
    }
    m = /[{,]\s*"((?:[^"\\]|\\.)*)$/.exec(before);
    if (m) {
      const options = Object.keys(samples.labels)
        .sort()
        .map((key) => ({
          label: key,
          type: "property",
          apply: (view, completion, from, to) => {
            const closed = view.state.sliceDoc(to, to + 1) === '"';
            const insert = JSON.stringify(key).slice(1, -1) + '": "';
            view.dispatch({
              changes: { from, to: closed ? to + 1 : to, insert: insert + (closed ? '"' : "") },
              selection: { anchor: from + insert.length },
            });
            startCompletion(view);
          },
        }));
      return { from: context.pos - m[1].length, options, validFor: /^[^"]*$/ };
    }
    return null;
  };
}

function matchSource(ctx) {
  const { startCompletion } = ctx.cm.autocomplete;
  return async (context) => {
    const line = context.state.doc.lineAt(context.pos);
    const before = line.text.slice(0, context.pos - line.from);
    const samples = await ctx.samples;
    const at = before.indexOf("=");

    if (at < 0) {
      const key = /\S*$/.exec(before)[0];
      if (!key && !context.explicit) return null;
      const options = Object.keys(samples.labels)
        .sort()
        .map((label) => ({
          label,
          type: "property",
          apply: (view, completion, from, to) => {
            view.dispatch({ changes: { from, to, insert: label + "=" }, selection: { anchor: from + label.length + 1 } });
            startCompletion(view);
          },
        }));
      return { from: context.pos - key.length, options, validFor: /^[^=\s]*$/ };
    }

    const values = samples.labels[before.slice(0, at).trim()] || [];
    const partial = before.slice(at + 1).replace(/^\s+/, "");
    return {
      from: context.pos - partial.length,
      options: values.map((label, i) => ({ label, type: "text", boost: -Math.min(i, 99) })),
      validFor: /^.*$/,
    };
  };
}
