"use strict";

/* =====================================================================
   Agent list, selection and the per-agent icon/accent picker.

   Depends on util.js for DOM refs, state, helpers and UI primitives.
   selectAgent() calls into stream.js/render.js at runtime, which is safe
   because classic scripts resolve functions when they are invoked.
   ===================================================================== */

function agentView(name) {
  const base = agents.find((a) => a.name === name) || { name: name || "Agent" };
  const override = visualOverrides[name] || {};
  return {
    name: base.name,
    description: base.description || "",
    state: base.state || "",
    icon: override.icon !== undefined ? override.icon : (base.icon || ""),
    accent: override.accent !== undefined ? override.accent : (base.accent || ""),
  };
}

function accentOf(view) {
  return { key: "moon", cls: "accent-moon" };
}

// Returns an HTML string so the same icon can be used in the sidebar, header,
// empty state and message rows.
function resolveAgentIcon(iconSpec, name) {
  const raw = String(iconSpec || "").trim();
  const monogram = escapeHtml(getMonogram(name));
  const monoHtml = `<span class="monogram">${monogram}</span>`;

  if (!raw || raw.toLowerCase() === "auto") return AGENT_ICONS.satellite;
  if (AGENT_ICONS[raw]) return AGENT_ICONS[raw];
  if (raw.startsWith("<svg")) return raw;
  if (/^(https?:\/\/|\/|\.\/|data:image\/)/i.test(raw)) {
    return `<img src="${escapeHtml(raw)}" alt="" onerror="this.outerHTML='&lt;span class=&quot;monogram&quot;&gt;${monogram}&lt;/span&gt;'">`;
  }
  return `<span style="font-size:14px;line-height:1">${escapeHtml(raw.slice(0, 4))}</span>`;
}

/* =====================================================================
   Agent list + selection
   ===================================================================== */
function updateActiveHighlight() {
  for (const item of agentList.children) {
    if (item.dataset && item.dataset.agent) {
      item.classList.toggle("active", item.dataset.agent === currentAgentName);
    }
  }
}

function renderAgentList() {
  const query = (agentSearch.value || "").trim().toLowerCase();
  const filtered = agents.filter((a) =>
    (a.name + " " + (a.description || "")).toLowerCase().includes(query)
  );

  agentList.replaceChildren();
  if (filtered.length === 0) {
    const empty = el("div", "no-agents");
    empty.textContent = "No matching agents.";
    agentList.appendChild(empty);
    return;
  }

  for (const info of filtered) {
    const view = agentView(info.name);
    const accent = accentOf(view);
    const item = el("div", "agent-item");
    item.setAttribute("role", "button");
    item.setAttribute("tabindex", "0");
    item.dataset.agent = info.name;

    const icon = el("div", "icon-box " + accent.cls);
    icon.innerHTML = resolveAgentIcon(view.icon, view.name);

    const text = el("div", "agent-text");
    const name = el("div", "agent-name");
    name.textContent = view.name;
    const desc = el("div", "agent-desc");
    desc.textContent = view.description;
    text.append(name, desc);

    item.append(icon, text);
    if (String(info.state || "").toLowerCase() === "busy") {
      item.appendChild(el("span", "state-dot"));
    }

    item.addEventListener("click", () => selectAgent(info.name));
    item.addEventListener("keydown", (event) => {
      if (event.target === item && (event.key === "Enter" || event.key === " ")) {
        event.preventDefault();
        selectAgent(info.name);
      }
    });
    agentList.appendChild(item);
  }
  updateActiveHighlight();
}

// Title block above the chat: icon tile, name and description of the agent.
function updateAgentHead() {
  const view = agentView(currentAgentName);
  const nameEl = document.getElementById("current-agent");
  const descEl = document.getElementById("header-agent-desc");
  const iconEl = document.getElementById("head-icon");
  if (nameEl) nameEl.textContent = currentAgentName || "Connecting…";
  if (descEl) descEl.textContent = view.description;
  if (iconEl) iconEl.innerHTML = resolveAgentIcon(view.icon, view.name);
}

function setCurrentAgent(name) {
  currentAgentName = name || "";
  promptEl.placeholder = `Message ${currentAgentName || "agent"}…`;
  updateAgentHead();
  updateActiveHighlight();
  renderEmptyState();
}

// Switching agents needs a fresh WebSocket; the server sends a new snapshot
// which rebuilds the whole conversation. No page reload required.
function selectAgent(name) {
  if (!name) return;
  closeLeftDrawer();
  if (name === selectedAgent) return;

  selectedAgent = name;
  try { window.localStorage.setItem("ceres-agent", name); } catch (err) { /* optional */ }
  setCurrentAgent(name);

  finalizeStream();
  resetToolBlocks();
  stagedImages = [];
  renderStagedImages();
  chatInner.replaceChildren();
  renderEmptyState();
  setTokenCount(0);
  updateWordCount();
  setWorkingState("idle");

  intentionalClose = true;
  if (ws) ws.close();
}

/* =====================================================================
    Agent icon & accent picker
    ===================================================================== */
function closeIconPicker() {
  iconPicker.classList.add("hidden");
}

// Opened from the pencil button at the top right. stopPropagation keeps the
// global document click handler (closeIconPicker) from closing it again.
function openIconPicker(event) {
  if (event) event.stopPropagation();
  renderIconPickerContent();
  iconPicker.classList.remove("hidden");
}

function renderIconPickerContent() {
  const view = agentView(currentAgentName);

  iconPresetGrid.replaceChildren();
  for (const [key, svg] of Object.entries(AGENT_ICONS)) {
    const isSelected = view.icon === key || (!view.icon && key === "satellite");
    const btn = el("button", "preset-btn" + (isSelected ? " selected" : ""));
    btn.type = "button";
    btn.title = key;
    btn.innerHTML = svg;
    btn.addEventListener("click", () => selectPresetIcon(key));
    iconPresetGrid.appendChild(btn);
  }

  customIconInput.value = view.icon;
}

function setOverride(field, value) {
  const name = currentAgentName;
  if (!name) return;
  visualOverrides[name] = Object.assign({}, visualOverrides[name], { [field]: value });
  saveJSON("ceres-visuals", visualOverrides);
  setCurrentAgent(name);
  renderAgentList();
}

function selectPresetIcon(key) {
  setOverride("icon", key);
  renderIconPickerContent();
  iconPicker.classList.remove("hidden");
}

function applyCustomIconInput(value) {
  setOverride("icon", value.trim());
}

function resetAgentVisuals() {
  if (!currentAgentName) return;
  delete visualOverrides[currentAgentName];
  saveJSON("ceres-visuals", visualOverrides);
  setCurrentAgent(currentAgentName);
  renderAgentList();
  renderIconPickerContent();
  iconPicker.classList.remove("hidden");
  showToast("Agent icon reset to default.");
}
