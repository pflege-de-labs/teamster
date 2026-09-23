// Turns the destination id fields into pickers when Graph answers. The inputs stay
// the source of truth, so an operator can still type ids whenever this fails.
(function () {
  const teamInput = document.getElementById("destination-team");
  const channelInput = document.getElementById("destination-channel");
  if (!teamInput || !channelInput) return;

  // The label wrapping the channel input, so the field can be hidden whole
  // rather than leaving a label above nothing.
  const channelField = channelInput.closest("label");
  const channelHint = document.getElementById("destination-channel-hint");

  // Set only when the server rendered the delegated-Teams toggle (ADR 0037):
  // the feature is configured on and this session has a Keycloak login
  // behind it. Absent that, this script behaves exactly as it did before the
  // toggle existed.
  const myTeamsAvailable = teamInput.dataset.pickerMyTeams === "true";
  let mode = "all";

  // The words come from the field the server rendered, which knows the
  // language; this script does not, and a catalog shipped to the browser would
  // be a second place for the text to live.
  function wordsOf(input) {
    return {
      label: input.dataset.pickerLabel || "",
      placeholder: input.dataset.pickerPlaceholder || "",
    };
  }

  function teamsEndpoint() {
    return mode === "mine" ? "/api/graph/my-teams" : "/api/graph/teams";
  }

  function channelsEndpoint(teamID) {
    const base = mode === "mine" ? "/api/graph/my-teams/" : "/api/graph/teams/";
    return base + encodeURIComponent(teamID) + "/channels";
  }

  function buildSelect(id, label, placeholder, items, current, input) {
    const select = document.createElement("select");
    select.id = id;
    select.setAttribute("aria-label", label);
    select.className = input.className;

    const empty = document.createElement("option");
    empty.value = "";
    empty.textContent = placeholder;
    select.appendChild(empty);

    items.forEach((item) => {
      const option = document.createElement("option");
      option.value = item.id;
      option.textContent = item.name;
      if (current && current === item.id) option.selected = true;
      select.appendChild(option);
    });

    return select;
  }

  function showInput(input, selectID) {
    const existing = document.getElementById(selectID);
    if (existing) existing.remove();
    input.type = "text";
  }

  // A channel belongs to a Team, so asking for one before a Team is chosen is
  // asking a question with no answers. The whole field goes away rather than
  // falling back to a text input nobody can fill in usefully.
  function hideChannelField(reason) {
    const existing = document.getElementById("destination-channel-select");
    if (existing) existing.remove();
    channelInput.type = "hidden";
    channelInput.value = "";
    if (channelField) channelField.hidden = true;
    if (channelHint) {
      channelHint.textContent = reason || "";
      channelHint.hidden = reason === "";
    }
  }

  function showChannelField() {
    if (channelField) channelField.hidden = false;
    if (channelHint) channelHint.hidden = true;
  }

  async function load(url, key) {
    let payload;
    try {
      const res = await fetch(url);
      if (!res.ok) return null;
      payload = await res.json();
    } catch (err) {
      return null;
    }
    const items = payload ? payload[key] : null;
    if (!Array.isArray(items) || items.length === 0) return null;
    return items;
  }

  async function loadChannels(teamID) {
    const channels = await load(channelsEndpoint(teamID), "channels");
    if (!channels) {
      // Graph could not list them, so typing an id is the way through again.
      showChannelField();
      showInput(channelInput, "destination-channel-select");
      return;
    }

    showChannelField();

    const select = buildSelect(
      "destination-channel-select",
      wordsOf(channelInput).label,
      wordsOf(channelInput).placeholder,
      channels,
      channelInput.value,
      channelInput,
    );
    select.addEventListener("change", () => {
      channelInput.value = select.value;
    });

    const existing = document.getElementById("destination-channel-select");
    if (existing) existing.remove();
    channelInput.parentNode.insertBefore(select, channelInput);
    channelInput.type = "hidden";
  }

  // loadTeams (re)builds the Team select for whatever `mode` currently is,
  // called once at start and again whenever the toggle switches modes.
  async function loadTeams() {
    const teams = await load(teamsEndpoint(), "teams");
    const existing = document.getElementById("destination-team-select");

    if (!teams) {
      if (existing) existing.remove();
      teamInput.type = "text";
      hideChannelField(channelInput.dataset.pickerRequiresTeam || "");
      return;
    }

    const current = teamInput.value;
    const select = buildSelect(
      "destination-team-select",
      wordsOf(teamInput).label,
      wordsOf(teamInput).placeholder,
      teams,
      current,
      teamInput,
    );
    select.addEventListener("change", () => {
      teamInput.value = select.value;
      if (!select.value) {
        hideChannelField(channelInput.dataset.pickerRequiresTeam || "");
        return;
      }
      loadChannels(select.value);
    });

    if (existing) existing.remove();
    teamInput.parentNode.insertBefore(select, teamInput);
    teamInput.type = "hidden";

    if (select.value) {
      teamInput.value = select.value;
      await loadChannels(select.value);
      return;
    }
    hideChannelField(channelInput.dataset.pickerRequiresTeam || "");
  }

  // buildModeToggle renders "All Teams"/"My Teams" radios above the Team
  // field, only when the server says the delegated list is available at all
  // for this session (see views.Page.BrokerAvailable).
  function buildModeToggle() {
    if (!myTeamsAvailable) return null;

    const wrapper = document.createElement("div");
    wrapper.className = "mt-1 flex gap-3 text-xs text-slate-600";

    function radio(value, label, checked) {
      const wrap = document.createElement("label");
      wrap.className = "inline-flex items-center gap-1";
      const input = document.createElement("input");
      input.type = "radio";
      input.name = "destination-team-mode";
      input.value = value;
      input.checked = checked;
      input.className = "h-3 w-3";
      input.addEventListener("change", () => {
        if (!input.checked) return;
        mode = value;
        loadTeams();
      });
      wrap.appendChild(input);
      wrap.appendChild(document.createTextNode(" " + label));
      return wrap;
    }

    wrapper.appendChild(radio("all", teamInput.dataset.pickerAllTeamsLabel || "All Teams", true));
    wrapper.appendChild(radio("mine", teamInput.dataset.pickerMyTeamsLabel || "My Teams", false));
    return wrapper;
  }

  (async () => {
    const toggle = buildModeToggle();
    if (toggle) teamInput.parentNode.insertBefore(toggle, teamInput);
    await loadTeams();
  })();
})();
