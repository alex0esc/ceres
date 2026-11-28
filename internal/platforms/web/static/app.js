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
  // Do not build the empty state here: the first snapshot arrives a few ms
  // later and would immediately remove the orbit again, causing a flash.
  updateWordCount();

  document.addEventListener("click", closeIconPicker);
  document.addEventListener("keydown", (event) => {
    if (event.defaultPrevented) return;
    if (event.key === "Escape") {
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
