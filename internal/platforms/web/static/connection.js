"use strict";

/* =====================================================================
   WebSocket, REST calls and composer input handling.

   Incoming frames are dispatched to stream.js; outgoing text and staged
   images go through POST /api/message. Commands are fetched once via
   GET /api/commands and handed to the autocomplete in commands.js.
   ===================================================================== */

function connect() {
  setConnectionState("connecting");
  const protocol = location.protocol === "https:" ? "wss:" : "ws:";
  const query = selectedAgent ? `?agent=${encodeURIComponent(selectedAgent)}` : "";
  const socket = new WebSocket(`${protocol}//${location.host}/ws${query}`);
  ws = socket;

  socket.onopen = () => {
    reconnectAttempts = 0;
    setConnectionState("connected");
    // A fresh socket means a new agent (or a reconnect). Refresh the header
    // counter right away instead of waiting for the next status poll tick.
    pollStatus();
  };
  socket.onerror = () => setConnectionState("disconnected");

  socket.onmessage = (event) => {
    let frame;
    try { frame = JSON.parse(event.data); } catch (err) { return; }

    switch (frame.type) {
      case "snapshot": applySnapshot(frame); break;
      case "delta": applyDelta(frame.entry); break;
      case "message_end":
        finalizeStream();
        updateWordCount();
        setWorkingState("idle");
        pollStatus();
        break;
      case "reset":
        finalizeStream();
        resetToolBlocks();
        chatInner.replaceChildren();
        renderEmptyState();
        setTokenCount(0);
        updateWordCount();
        break;
      case "status":
        if (frame.compressing) setWorkingState("compressing");
        break;
      case "error":
        showError(frame.message);
        break;
    }
  };

  socket.onclose = () => {
    setConnectionState("disconnected");

    // Switching agents closes the socket on purpose; that must not count as a
    // failed attempt and should reconnect immediately.
    if (intentionalClose) {
      intentionalClose = false;
      reconnectAttempts = 0;
      connect();
      return;
    }

    // Give up after a few failures instead of flipping between states forever.
    if (reconnectAttempts >= MAX_RECONNECT_ATTEMPTS) {
      syncPill.title = "Connection lost — retry limit reached";
      return;
    }
    reconnectAttempts++;
    window.setTimeout(connect, 1000 * reconnectAttempts);
  };
}

// Rebuild the whole chat from a fresh snapshot. Needed when a display setting
// changes which history entries are rendered (e.g. the reasoning toggle):
// skipped entries never entered the DOM, so a CSS change alone cannot bring
// them back. Closing intentionally reconnects immediately.
function reloadChat() {
  finalizeStream();
  resetToolBlocks();
  chatInner.replaceChildren();
  renderEmptyState();
  updateWordCount();
  intentionalClose = true;
  if (ws) ws.close();
  else connect();
}

async function postText(text, images, showResult) {
  const response = await fetch("/api/message", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ text, agent: selectedAgent, images: images || [] }),
  });
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  if (response.status !== 204) {
    const data = await response.json();
    if (data.command_result && showResult) appendCommandResult(data.command_result);
  }
}

async function pollStatus() {
  const name = selectedAgent || currentAgentName;
  if (!name) return;
  try {
    const response = await fetch("/api/status?agent=" + encodeURIComponent(name));
    if (!response.ok) return;
    const data = await response.json();
    setTokenCount(data.tokens || 0);
    if (!sending) {
      if (data.compressing) setWorkingState("compressing");
      else if (String(data.state || "").toLowerCase() === "busy") setWorkingState("busy");
      else setWorkingState("idle");
    }
  } catch (err) {
    /* polling is best-effort */
  }
}

// The command tree is fetched once on page load; the autocomplete logic lives
// in commands.js and consumes the plain {name, description, subcommands} shape.
async function loadCommands() {
  try {
    const response = await fetch("/api/commands");
    if (!response.ok) return;
    const data = await response.json();
    if (commandAC) commandAC.setCommands(data.commands || []);
  } catch (err) {
    /* autocomplete is optional */
  }
}

async function loadAgents() {
  try {
    const response = await fetch("/api/agents");
    if (!response.ok) return;
    const data = await response.json();
    agents = data.agents || [];

    if (selectedAgent && !agents.some((a) => a.name === selectedAgent)) {
      selectedAgent = "";
      try { window.localStorage.removeItem("ceres-agent"); } catch (err) { /* optional */ }
      intentionalClose = true;
      if (ws) ws.close();
    }

    renderAgentList();
    setCurrentAgent(selectedAgent || data.current || "");
  } catch (err) {
    // The sidebar is informational; a failed load is not fatal.
  }
}

async function sendMessage() {
  const text = promptEl.value.trim();
  const images = stagedImages.slice();
  if ((!text && images.length === 0) || sending) return;

  sending = true;
  sendButton.disabled = true;
  setWorkingState("busy");
  promptEl.value = "";
  promptEl.style.height = "auto";
  composer.classList.remove("expanded");
  stagedImages = [];
  renderStagedImages();
  if (commandAC) commandAC.update();

  try {
    await postText(text, images, true);
  } catch (err) {
    showError(`Could not send message: ${err.message}`);
  } finally {
    sending = false;
    sendButton.disabled = false;
    promptEl.focus();
    pollStatus();
  }
}

async function interruptAgent() {
  try {
    await postText("/interrupt", [], false);
  } catch (err) {
    showError(`Could not interrupt: ${err.message}`);
  }
}

composer.addEventListener("submit", (event) => {
  event.preventDefault();
  sendMessage();
});

promptEl.addEventListener("input", () => {
  promptEl.style.height = "auto";
  const wasExpanded = composer.classList.contains("expanded");
  const height = promptEl.scrollHeight;

  // Once multi-line, keep the full-width layout until the prompt is cleared.
  // Re-measuring after the toggle avoids a narrow/wide oscillation loop.
  if (!wasExpanded && height > 48) composer.classList.add("expanded");
  else if (wasExpanded && promptEl.value.trim() === "") composer.classList.remove("expanded");

  promptEl.style.height = "auto";
  promptEl.style.height = Math.min(promptEl.scrollHeight, 250) + "px";
  if (commandAC) commandAC.update();
});

promptEl.addEventListener("keydown", (event) => {
  if (commandAC && commandAC.visible) {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      commandAC.move(1);
      return;
    }
    if (event.key === "ArrowUp") {
      event.preventDefault();
      commandAC.move(-1);
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      commandAC.hide();
      return;
    }
    if (event.key === "Tab") {
      event.preventDefault();
      commandAC.accept();
      return;
    }
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      if (commandAC.accept()) return;
    }
  }

  if (event.key === "Enter" && !event.shiftKey) {
    event.preventDefault();
    composer.requestSubmit();
  }
});
