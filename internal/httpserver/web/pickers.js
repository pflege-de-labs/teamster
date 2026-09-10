// Turns the destination id fields into pickers when Graph answers. The inputs stay
// the source of truth, so an operator can still type ids whenever this fails.
(function () {
  const teamInput = document.getElementById("destination-team");
  const channelInput = document.getElementById("destination-channel");
  if (!teamInput || !channelInput) return;

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
    const channels = await load("/api/graph/teams/" + encodeURIComponent(teamID) + "/channels", "channels");
    if (!channels) {
      showInput(channelInput, "destination-channel-select");
      return;
    }

    const select = buildSelect(
      "destination-channel-select",
      "Channel",
      "— choose a channel —",
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

  (async () => {
    const teams = await load("/api/graph/teams", "teams");
    if (!teams) return;

    const select = buildSelect(
      "destination-team-select",
      "Team",
      "— choose a team —",
      teams,
      teamInput.value,
      teamInput,
    );
    select.addEventListener("change", () => {
      teamInput.value = select.value;
      if (!select.value) {
        showInput(channelInput, "destination-channel-select");
        return;
      }
      loadChannels(select.value);
    });

    teamInput.parentNode.insertBefore(select, teamInput);
    teamInput.type = "hidden";

    if (select.value) {
      teamInput.value = select.value;
      await loadChannels(select.value);
    }
  })();
})();
