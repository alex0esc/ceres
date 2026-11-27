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
  const key = ACCENTS.includes(view.accent) ? view.accent : ACCENTS[hashString(view.name) % ACCENTS.length];
  return { key, cls: "accent-" + key, dot: "dot-" + key };
}

// Returns an HTML string so the same icon can be used in the sidebar, header,
// empty state and message rows.
function resolveAgentIcon(iconSpec, name) {
  const raw = String(iconSpec || "").trim();
  const monogram = escapeHtml(getMonogram(name));
  const monoHtml = `<span class="monogram">${monogram}</span>`;

  if (!raw || raw.toLowerCase() === "auto") return monoHtml;
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
    const item = el("button", "agent-item");
    item.type = "button";
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
      item.appendChild(el("span", "state-dot " + accent.dot));
    }
    item.addEventListener("click", () => selectAgent(info.name));
    agentList.appendChild(item);
  }
  updateActiveHighlight();
}

function setCurrentAgent(name) {
  currentAgentName = name || "";
  const view = agentView(currentAgentName);
  currentAgentEl.textContent = currentAgentName || "No agent connected";
  headerDescEl.textContent = view.description;
  promptEl.placeholder = `Message ${currentAgentName || "agent"}…`;
  const accent = accentOf(view);
  headerAvatarEl.className = "icon-box agent-avatar " + accent.cls;
  headerAvatarEl.innerHTML = resolveAgentIcon(view.icon, view.name);
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
function toggleIconPicker(event) {
  if (event) event.stopPropagation();
  const hidden = iconPicker.classList.contains("hidden");
  if (hidden) renderIconPickerContent();
  iconPicker.classList.toggle("hidden", !hidden);
}

function closeIconPicker() {
  iconPicker.classList.add("hidden");
}

function renderIconPickerContent() {
  const view = agentView(currentAgentName);

  iconPresetGrid.replaceChildren();
  for (const [key, svg] of Object.entries(AGENT_ICONS)) {
    const btn = el("button", "preset-btn" + (view.icon === key ? " selected" : ""));
    btn.type = "button";
    btn.title = key;
    btn.innerHTML = svg;
    btn.addEventListener("click", () => selectPresetIcon(key));
    iconPresetGrid.appendChild(btn);
  }

  customIconInput.value = view.icon;

  accentRow.replaceChildren();
  for (const key of ACCENTS) {
    const btn = el("button", "accent-swatch" + (accentOf(view).key === key ? " selected" : ""));
    btn.type = "button";
    btn.title = key;
    const dot = el("span", "dot-" + key);
    btn.appendChild(dot);
    btn.addEventListener("click", () => selectAgentAccent(key));
    accentRow.appendChild(btn);
  }
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

function selectAgentAccent(key) {
  setOverride("accent", key);
  renderIconPickerContent();
  iconPicker.classList.remove("hidden");
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
