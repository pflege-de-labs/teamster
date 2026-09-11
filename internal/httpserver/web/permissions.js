// The permissions tree: Teams with their channels nested under them, a checkbox
// at each level, and one at the top for all of them.
//
// A Team ticked whole is stored as a grant on the Team, which covers channels
// added later. Channels ticked individually are stored one by one. That
// difference is the reason the tree has three states rather than two.
(function () {
  const tree = document.getElementById("permissions-tree");
  if (!tree) return;

  const roleSelect = document.getElementById("permissions-role");
  const saveButton = document.getElementById("permissions-save");
  const allBox = document.getElementById("permissions-all");
  const status = document.getElementById("permissions-status");

  const words = tree.dataset;
  // teams is the loaded shape: { id, name, channels: [{id, name}] | null }.
  let teams = [];

  function say(message) {
    if (status) status.textContent = message || "";
  }

  async function fetchJSON(url, options) {
    const res = await fetch(url, options);
    if (!res.ok) {
      let detail = "status " + res.status;
      try {
        const payload = await res.json();
        if (payload && payload.error) detail = payload.error;
      } catch (err) {
        // The status is all there is to report.
      }
      throw new Error(detail);
    }
    return res.json();
  }

  function teamBox(teamID) {
    return tree.querySelector('input[data-team="' + CSS.escape(teamID) + '"][data-channel=""]');
  }

  function channelBoxes(teamID) {
    return Array.from(tree.querySelectorAll('input[data-team="' + CSS.escape(teamID) + '"][data-channel]:not([data-channel=""])'));
  }

  // A Team is ticked when all of its channels are, indeterminate when some are.
  // With the channels not loaded yet, the Team's own box is the whole answer.
  function refreshTeam(teamID) {
    const box = teamBox(teamID);
    const channels = channelBoxes(teamID);
    if (!box || channels.length === 0) return;

    const checked = channels.filter((channel) => channel.checked).length;
    box.checked = checked === channels.length;
    box.indeterminate = checked > 0 && checked < channels.length;
  }

  function refreshAll() {
    if (!allBox) return;
    const boxes = Array.from(tree.querySelectorAll('input[data-channel=""]'));
    const checked = boxes.filter((box) => box.checked).length;
    allBox.checked = boxes.length > 0 && checked === boxes.length;
    allBox.indeterminate = checked > 0 && checked < boxes.length;
  }

  function renderChannels(container, team) {
    container.replaceChildren();

    if (!team.channels) {
      const note = document.createElement("p");
      note.className = "py-2 pl-3 pr-4 text-xs text-amber-700";
      note.textContent = words.channelsFailed || "";
      container.appendChild(note);
      return;
    }

    team.channels.forEach((channel) => {
      const label = document.createElement("label");
      label.className = "flex items-center gap-2 py-1.5 pl-3 pr-4 text-sm";

      const box = document.createElement("input");
      box.type = "checkbox";
      box.className = "rounded border-slate-300";
      box.dataset.team = team.id;
      box.dataset.channel = channel.id;
      box.checked = team.granted === "all" || (team.grantedChannels || []).includes(channel.id);
      box.addEventListener("change", () => {
        refreshTeam(team.id);
        refreshAll();
      });

      const name = document.createElement("span");
      name.textContent = channel.name;

      label.append(box, name);
      container.appendChild(label);
    });

    refreshTeam(team.id);
    refreshAll();
  }

  async function loadChannels(team, container) {
    if (team.channels !== undefined) {
      renderChannels(container, team);
      return;
    }

    try {
      const payload = await fetchJSON("/api/graph/teams/" + encodeURIComponent(team.id) + "/channels");
      team.channels = Array.isArray(payload.channels) ? payload.channels : [];
    } catch (err) {
      team.channels = null;
    }
    renderChannels(container, team);
  }

  function renderTeam(team) {
    const details = document.createElement("details");
    details.className = "px-0 py-1";

    const summary = document.createElement("summary");
    summary.className = "flex cursor-pointer items-center gap-2 px-4 py-2 text-sm";

    const box = document.createElement("input");
    box.type = "checkbox";
    box.className = "rounded border-slate-300";
    box.dataset.team = team.id;
    box.dataset.channel = "";
    box.checked = team.granted === "all";
    box.indeterminate = team.granted === "some";
    // Clicking the box must not open the disclosure it sits in.
    box.addEventListener("click", (event) => event.stopPropagation());
    box.addEventListener("change", () => {
      // A Team ticked or cleared takes its channels with it.
      channelBoxes(team.id).forEach((channel) => {
        channel.checked = box.checked;
      });
      box.indeterminate = false;
      refreshAll();
    });

    const name = document.createElement("span");
    name.className = "font-medium";
    name.textContent = team.name;

    summary.append(box, name);
    details.appendChild(summary);

    // Indented past the Team's own checkbox, with a rule down the left, so a
    // channel reads as belonging to the Team above it rather than as another
    // row in a flat list.
    const channels = document.createElement("div");
    channels.className = "ml-9 border-l border-slate-200 pb-2";
    details.appendChild(channels);

    details.addEventListener("toggle", () => {
      if (details.open) loadChannels(team, channels);
    });

    return details;
  }

  function render() {
    tree.replaceChildren();
    teams.forEach((team) => tree.appendChild(renderTeam(team)));
    refreshAll();
  }

  // What the tree says, as the scopes the API stores: a whole Team, or the
  // channels ticked in it.
  function scopes() {
    const out = [];
    teams.forEach((team) => {
      const box = teamBox(team.id);
      if (!box) return;

      if (box.checked && !box.indeterminate) {
        out.push({ team_id: team.id, channel_id: "" });
        return;
      }
      channelBoxes(team.id)
        .filter((channel) => channel.checked)
        .forEach((channel) => out.push({ team_id: team.id, channel_id: channel.dataset.channel }));
    });
    return out;
  }

  // The grants already stored decide what is ticked when a role is chosen. A
  // grant on a Team whose channels are not loaded yet is remembered as "all",
  // which is what it means.
  function applyGrants(grants, role) {
    const forRole = (grants || []).filter((grant) => grant.role === role);
    teams.forEach((team) => {
      const mine = forRole.filter((grant) => grant.team_id === team.id);
      const whole = mine.some((grant) => !grant.channel_id);
      team.granted = whole ? "all" : mine.length > 0 ? "some" : "none";
      team.grantedChannels = mine.filter((grant) => grant.channel_id).map((grant) => grant.channel_id);
      // Channels are reloaded so their boxes reflect the new role.
      if (team.channels !== undefined && team.channels !== null) team.channels = team.channels;
    });
    render();
  }

  async function loadRole() {
    say("");
    try {
      const payload = await fetchJSON("/api/grants");
      applyGrants(payload, roleSelect ? roleSelect.value : "");
    } catch (err) {
      say(err.message);
    }
  }

  if (allBox) {
    allBox.addEventListener("change", () => {
      tree.querySelectorAll("input[data-team]").forEach((box) => {
        box.checked = allBox.checked;
        box.indeterminate = false;
      });
      allBox.indeterminate = false;
    });
  }

  if (roleSelect) roleSelect.addEventListener("change", loadRole);

  if (saveButton) {
    saveButton.addEventListener("click", async () => {
      saveButton.disabled = true;
      try {
        await fetchJSON("/api/grants/role", {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ role: roleSelect ? roleSelect.value : "", scopes: scopes() }),
        });
        say(words.saved || "");
      } catch (err) {
        say((words.saveFailed || "{0}").replace("{0}", err.message));
      } finally {
        saveButton.disabled = false;
      }
    });
  }

  (async () => {
    try {
      const payload = await fetchJSON("/api/graph/teams");
      teams = Array.isArray(payload.teams) ? payload.teams : [];
    } catch (err) {
      tree.replaceChildren();
      const note = document.createElement("p");
      note.className = "px-4 py-6 text-sm text-amber-800";
      note.textContent = words.loadFailed || "";
      tree.appendChild(note);
      return;
    }
    await loadRole();
  })();
})();
