// Fills the Versions menu from versions.json at the site root, so a version built long ago
// still lists the releases that came after it.
(() => {
  const meta = (name) => document.querySelector(`meta[name="${name}"]`)?.content ?? "";
  const current = meta("docs-version");
  const root = meta("docs-root");
  // Labels in the page's language, from i18n/<lang>.yaml; %s takes a version.
  const t = JSON.parse(meta("docs-i18n") || "{}");
  const fill = (text, value) => (text ?? "").replace("%s", value);

  const pathInVersion = () => {
    const prefix = `${root}${current}/`;
    const path = location.pathname;
    return path.startsWith(prefix) ? path.slice(prefix.length) : "";
  };

  const renderMenu = (versions) => {
    const toggle = document.querySelector(
      `.hextra-nav-menu-toggle[aria-label="${CSS.escape(t.versions ?? "Versions")}"]`,
    );
    const list = toggle?.parentElement.querySelector(".hextra-nav-menu-items");
    const template = list?.querySelector("li");
    if (!template) return;

    toggle.querySelector("span.hx\\:text-center").textContent = current;
    const page = pathInVersion();
    list.replaceChildren(
      ...versions.map((v) => {
        const item = template.cloneNode(true);
        const link = item.querySelector("a");
        link.removeAttribute("target");
        link.href = `${root}${v.path}${page}`;
        link.textContent = v.latest ? `${v.version} (latest)` : v.version;
        if (v.version === current) link.setAttribute("aria-current", "page");
        return item;
      }),
    );
  };

  const renderBanner = (versions) => {
    const latest = versions.find((v) => v.latest);
    if (!latest || current === latest.version) return;

    let message;
    if (current === "dev") {
      message = t.dev;
    } else if (current.startsWith("pr-")) {
      message = t.preview;
    } else {
      message = fill(t.old, current);
    }
    const banner = document.createElement("div");
    banner.className = "docs-version-banner";
    banner.append(`${message} `);
    const link = document.createElement("a");
    link.href = `${root}${latest.path}`;
    link.textContent = fill(t.goTo, latest.version);
    banner.append(link);
    document.body.prepend(banner);
  };

  fetch(`${root}versions.json`)
    .then((r) => (r.ok ? r.json() : []))
    .catch(() => [])
    .then((versions) => {
      renderMenu(versions);
      renderBanner(versions);
    });
})();
