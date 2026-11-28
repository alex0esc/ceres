"use strict";

/* =====================================================================
   Bootstrap.

   All behaviour lives in the focused modules loaded before this one:
   util, render, image, agents, commands, stream and connection. This file only
   builds the autocomplete controller and wires up the initial state.
   ===================================================================== */
function init() {
  commandAC = new CommandAutocomplete(promptEl, cmdPopup);
  updateReasoningToggle();
  setWorkingState("idle");
  renderEmptyState();
  updateWordCount();

  document.addEventListener("click", closeIconPicker);
  document.addEventListener("keydown", (event) => {
    if (event.defaultPrevented) return;
    if (event.key === "Escape") {
      if (isAgentMenuOpen()) { event.preventDefault(); closeAgentMenu(true); return; }
      closeIconPicker();
      closeConfirmModal();
      closeLeftDrawer();
    }
  });

  loadAgents();
  loadCommands();
  connect();
  pollStatus();
  window.setInterval(pollStatus, 2000);
}

init();
