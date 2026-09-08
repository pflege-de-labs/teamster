const api = {
  templates: "/api/templates",
  destinations: "/api/destinations",
  routes: "/api/routes",
};

async function request(url, options = {}) {
  const res = await fetch(url, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error || "Request failed");
  }
  return res.json();
}

function renderList(list, container, onEdit, onDelete) {
  container.innerHTML = "";
  list.forEach((item) => {
    const li = document.createElement("li");
    li.innerHTML = `
      <strong>${item.name || item.id}</strong>
      <small>${item.id}</small>
      <div class="actions"></div>
    `;
    const actions = li.querySelector(".actions");
    const edit = document.createElement("button");
    edit.textContent = "Edit";
    edit.addEventListener("click", () => onEdit(item));
    const del = document.createElement("button");
    del.textContent = "Delete";
    del.addEventListener("click", () => onDelete(item.id));
    actions.append(edit, del);
    container.appendChild(li);
  });
}

async function loadTemplates() {
  const list = await request(api.templates);
  renderList(
    list,
    document.getElementById("template-list"),
    (item) => {
      document.getElementById("template-id").value = item.id;
      document.getElementById("template-name").value = item.name || "";
      document.getElementById("template-body").value = item.body || "";
    },
    async (id) => {
      await request(`${api.templates}/${id}`, { method: "DELETE" });
      loadTemplates();
    }
  );
}

async function loadDestinations() {
  const list = await request(api.destinations);
  renderList(
    list,
    document.getElementById("destination-list"),
    (item) => {
      document.getElementById("destination-id").value = item.id;
      document.getElementById("destination-name").value = item.name || "";
      document.getElementById("destination-team").value = item.team_id || "";
      document.getElementById("destination-channel").value = item.channel_id || "";
    },
    async (id) => {
      await request(`${api.destinations}/${id}`, { method: "DELETE" });
      loadDestinations();
    }
  );
}

async function loadRoutes() {
  const list = await request(api.routes);
  renderList(
    list,
    document.getElementById("route-list"),
    (item) => {
      document.getElementById("route-id").value = item.id;
      document.getElementById("route-name").value = item.name || "";
      document.getElementById("route-selector").value = JSON.stringify(
        item.label_selector || {}
      );
      document.getElementById("route-destination").value = item.destination_id || "";
      document.getElementById("route-template").value = item.template_id || "";
      document.getElementById("route-priority").value = item.priority || 0;
      document.getElementById("route-default").value = item.is_default
        ? "true"
        : "false";
    },
    async (id) => {
      await request(`${api.routes}/${id}`, { method: "DELETE" });
      loadRoutes();
    }
  );
}

function bindForms() {
  document.getElementById("template-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const id = document.getElementById("template-id").value;
    const payload = {
      name: document.getElementById("template-name").value,
      body: document.getElementById("template-body").value,
    };
    if (id) {
      await request(`${api.templates}/${id}`, {
        method: "PUT",
        body: JSON.stringify(payload),
      });
    } else {
      await request(api.templates, {
        method: "POST",
        body: JSON.stringify(payload),
      });
    }
    e.target.reset();
    loadTemplates();
  });

  document
    .getElementById("destination-form")
    .addEventListener("submit", async (e) => {
      e.preventDefault();
      const id = document.getElementById("destination-id").value;
      const payload = {
        name: document.getElementById("destination-name").value,
        team_id: document.getElementById("destination-team").value,
        channel_id: document.getElementById("destination-channel").value,
      };
      if (id) {
        await request(`${api.destinations}/${id}`, {
          method: "PUT",
          body: JSON.stringify(payload),
        });
      } else {
        await request(api.destinations, {
          method: "POST",
          body: JSON.stringify(payload),
        });
      }
      e.target.reset();
      loadDestinations();
    });

  document.getElementById("route-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const id = document.getElementById("route-id").value;
    let selector = {};
    try {
      selector = JSON.parse(document.getElementById("route-selector").value || "{}");
    } catch (err) {
      alert("Label selector must be valid JSON");
      return;
    }

    const payload = {
      name: document.getElementById("route-name").value,
      label_selector: selector,
      destination_id: document.getElementById("route-destination").value,
      template_id: document.getElementById("route-template").value,
      priority: Number(document.getElementById("route-priority").value || 0),
      is_default: document.getElementById("route-default").value === "true",
    };

    if (id) {
      await request(`${api.routes}/${id}`, {
        method: "PUT",
        body: JSON.stringify(payload),
      });
    } else {
      await request(api.routes, {
        method: "POST",
        body: JSON.stringify(payload),
      });
    }
    e.target.reset();
    loadRoutes();
  });
}

bindForms();
loadTemplates();
loadDestinations();
loadRoutes();
