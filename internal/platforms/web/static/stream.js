"use strict";

/* =====================================================================
   Streaming lifecycle and snapshot/delta application.

   Reparse the whole partial text, throttle via rAF, freeze finished messages
   and never touch them again. Depends on render.js for the DOM builders and
   util.js for state and UI primitives.
   ===================================================================== */

function nearBottom() {
  return chat.scrollHeight - chat.scrollTop - chat.clientHeight < 80;
}

function scrollToBottom() {
  chat.scrollTop = chat.scrollHeight;
}

function startStream(type) {
  streamType = type;
  const message = makeMessage(ENTRY[type] || "assistant", type);
  streamBubble = message.body;
  streamBubble.closest(".msg-row").classList.add("streaming");
  rawMarkdown = "";
}

function finalizeStream() {
  if (!streamBubble) return;
  if (renderFrame) {
    cancelAnimationFrame(renderFrame);
    renderFrame = 0;
  }
  renderMarkdownInto(streamBubble, rawMarkdown);
  const row = streamBubble.closest(".msg-row");
  if (row) {
    row.classList.remove("streaming");
    stampMessageTime(row, Date.now());
  }
  streamBubble = null;
  streamType = null;
  rawMarkdown = "";
}

function scheduleStreamRender() {
  if (renderFrame) return;
  renderFrame = requestAnimationFrame(() => {
    renderFrame = 0;
    if (!streamBubble) return;
    const stick = nearBottom();
    renderMarkdownInto(streamBubble, rawMarkdown, false);
    if (stick) scrollToBottom();
  });
}

function applySnapshot(frame) {
  finalizeStream();
  resetToolBlocks();
  chatInner.replaceChildren();
  setCurrentAgent(frame.agent || "");

  for (const entry of frame.entries || []) {
    if (!showReasoning && entry.type === 2) continue;
    const bubble = frozenBubble(entry);
    if (bubble) chatInner.appendChild(bubble);
  }

  // Tokens already streaming when the socket connected are replayed as a
  // partial message, so the view matches what other clients see.
  for (const entry of frame.partial || []) {
    if (!showReasoning && entry.type === 2) continue;
    if (TEXT_TYPES.has(entry.type)) {
      if (streamBubble && streamType !== entry.type) finalizeStream();
      if (!streamBubble) startStream(entry.type);
      rawMarkdown += entryText(entry);
    } else {
      finalizeStream();
      const bubble = frozenBubble(entry);
      if (bubble) chatInner.appendChild(bubble);
    }
  }

  if (streamBubble) renderMarkdownInto(streamBubble, rawMarkdown);
  // The snapshot is the only place that builds the empty state. Rendering it
  // earlier (on load, on agent switch, on reset) made the orbit flash briefly.
  renderEmptyState();
  updateWordCount();
  if (frame.compressing) setWorkingState("compressing");
  scrollToBottom();
}

function applyDelta(entry) {
  if (!entry) return;
  if (!showReasoning && entry.type === 2) return;
  // Live tokens mean the agent is working (also when started by a wakeup).
  // setWorkingState is idempotent, so this never restarts the animation.
  if (workingState === "idle") setWorkingState("busy");
  const stick = nearBottom();

  if (TEXT_TYPES.has(entry.type)) {
    if (streamBubble && streamType !== entry.type) finalizeStream();
    if (!streamBubble) startStream(entry.type);
    rawMarkdown += entryText(entry);
    scheduleStreamRender();
  } else {
    finalizeStream();
    const bubble = frozenBubble(entry);
    if (bubble) chatInner.appendChild(bubble);
  }

  if (stick) scrollToBottom();
}
