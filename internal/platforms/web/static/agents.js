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

  if (!raw || raw.toLowerCase() === "auto") return AGENT_ICONS.bot;
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
  closeAgentMenu(false);
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

    const moreBtn = el("button", "agent-more");
    moreBtn.type = "button";
    moreBtn.setAttribute("aria-label", "Actions for " + view.name);
    moreBtn.setAttribute("aria-expanded", "false");
    moreBtn.setAttribute("aria-controls", "agent-menu-pop");
    moreBtn.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor" aria-hidden="true"><circle cx="12" cy="5" r="2"/><circle cx="12" cy="12" r="2"/><circle cx="12" cy="19" r="2"/></svg>';
    moreBtn.addEventListener("click", (event) => {
      event.stopPropagation();
      toggleAgentMenu(moreBtn, view.name);
    });
    item.appendChild(moreBtn);

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

function setCurrentAgent(name) {
  currentAgentName = name || "";
  const view = agentView(currentAgentName);
  promptEl.placeholder = `Message ${currentAgentName || "agent"}…`;
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
    Agent row "⋯" actions menu (plain popover, not an ARIA menu)
    ===================================================================== */
let agentMenuTrigger = null;
let agentMenuTarget = "";

function isAgentMenuOpen() {
  const pop = document.getElementById("agent-menu-pop");
  return !!pop && !pop.hidden;
}

function toggleAgentMenu(trigger, name) {
  if (isAgentMenuOpen() && agentMenuTrigger === trigger) {
    closeAgentMenu(true);
    return;
  }
  openAgentMenu(trigger, name);
}

function openAgentMenu(trigger, name) {
  closeAgentMenu(false);
  const pop = document.getElementById("agent-menu-pop");
  agentMenuTrigger = trigger;
  agentMenuTarget = name;
  trigger.setAttribute("aria-expanded", "true");
  pop.style.visibility = "hidden";
  pop.hidden = false; // make visible in order to measure
  const r = trigger.getBoundingClientRect();
  const w = pop.offsetWidth, h = pop.offsetHeight;
  let left = Math.min(r.right - w, window.innerWidth - w - 8);
  left = Math.max(8, left);
  let top = r.bottom + 4;
  if (top + h > window.innerHeight - 8) top = Math.max(8, r.top - h - 4);
  pop.style.left = left + "px";
  pop.style.top = top + "px";
  pop.style.visibility = "";
  const first = pop.querySelector(".agent-menu-item");
  if (first) first.focus();
}

function closeAgentMenu(returnFocus) {
  const pop = document.getElementById("agent-menu-pop");
  if (pop) pop.hidden = true;
  if (agentMenuTrigger) {
    agentMenuTrigger.setAttribute("aria-expanded", "false");
    if (returnFocus && document.contains(agentMenuTrigger)) agentMenuTrigger.focus();
  }
  agentMenuTrigger = null;
  agentMenuTarget = "";
}

// Menu item clicks are delegated on the popover so the buttons can be
// re-created freely. stopPropagation() prevents the global document
// click handler (closeIconPicker) from firing at the same time.
document.getElementById("agent-menu-pop").addEventListener("click", (event) => {
  const item = event.target.closest(".agent-menu-item");
  if (!item) return;
  event.stopPropagation();
  const name = agentMenuTarget;
  const action = item.dataset.action;
  closeAgentMenu(false);
  if (!name) return;
  selectAgent(name);
  if (action === "clear") {
    promptResetCurrentChat();
  } else if (action === "icon") {
    renderIconPickerContent();
    iconPicker.classList.remove("hidden");
  }
});

// Outside click closes the menu, but clicks on a different "⋯" button
// toggle instead (handled by each button's own listener).
document.addEventListener("click", (event) => {
  if (!isAgentMenuOpen()) return;
  if (event.target.closest("#agent-menu-pop") || event.target.closest(".agent-more")) return;
  closeAgentMenu(false);
});

/* =====================================================================
    Agent icon & accent picker
    ===================================================================== */
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
