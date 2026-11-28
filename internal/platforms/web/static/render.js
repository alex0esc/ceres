"use strict";

/* =====================================================================
   Markdown / math rendering and every DOM builder for chat messages.

   The browser renders raw history.Entry JSON itself; Go never builds HTML
   for messages. Depends on util.js (helpers, agent view) and AGENT_ICONS.
   ===================================================================== */

function escapeMathText(text) {
  return String(text).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

const mathExtensions = [
  {
    name: "mathBlock",
    level: "block",
    start(src) { return src.indexOf("$$"); },
    tokenizer(src) {
      const match = /^\$\$[ \t]*(?:\r?\n)?([\s\S]*?)\$\$[ \t]*(?:\r?\n|$)/.exec(src);
      if (!match) return;
      return { type: "mathBlock", raw: match[0], text: match[0] };
    },
    renderer(token) { return escapeMathText(token.text); },
  },
  {
    name: "mathInline",
    level: "inline",
    start(src) { return src.indexOf("$"); },
    tokenizer(src) {
      if (src.startsWith("$$")) return;
      const match = /^\$([^$\n]+?)\$/.exec(src);
      if (!match) return;
      return { type: "mathInline", raw: match[0], text: match[0] };
    },
    renderer(token) { return escapeMathText(token.text); },
  },
];

if (window.marked) window.marked.use({ extensions: mathExtensions });

function enhanceCodeBlocks(root) {
  for (const pre of Array.from(root.querySelectorAll("pre"))) {
    if (pre.parentElement && pre.parentElement.classList.contains("code-wrap")) continue;
    const wrap = el("div", "code-wrap");
    const head = el("div", "code-head");
    const code = pre.querySelector("code");
    const match = code && code.className.match(/language-([\w-]+)/);
    const lang = el("span", "", match ? match[1] : "code");
    const copy = el("button", "code-copy");
    copy.type = "button";
    copy.textContent = "Copy";
    copy.dataset.code = pre.textContent;
    copy.addEventListener("click", () => copyCodeBlock(copy));
    head.append(lang, copy);
    pre.replaceWith(wrap);
    wrap.append(head, pre);
  }
}

function renderMarkdownInto(node, text, highlight = true) {
  if (!window.marked || !window.DOMPurify) {
    node.textContent = text || "";
    return;
  }
  node.innerHTML = window.DOMPurify.sanitize(window.marked.parse(text || ""));
  if (!highlight) return;

  if (window.hljs) {
    for (const code of node.querySelectorAll("pre code")) window.hljs.highlightElement(code);
  }
  if (typeof window.renderMathInElement === "function") {
    window.renderMathInElement(node, {
      delimiters: [
        { left: "$$", right: "$$", display: true },
        { left: "$", right: "$", display: false },
        { left: "\\(", right: "\\)", display: false },
        { left: "\\[", right: "\\]", display: true },
      ],
      ignoredTags: ["script", "noscript", "style", "textarea", "pre", "code"],
      throwOnError: false,
    });
  }
  enhanceCodeBlocks(node);
}

function copyCodeBlock(button) {
  const text = button.dataset.code || "";
  copyToClipboard(text);
  const previous = button.textContent;
  button.textContent = "Copied!";
  window.setTimeout(() => { button.textContent = previous; }, 1500);
}

function copyToClipboard(text) {
  const area = el("textarea");
  area.value = text;
  area.style.position = "fixed";
  area.style.opacity = "0";
  document.body.appendChild(area);
  area.select();
  try { document.execCommand("copy"); } catch (err) { /* optional */ }
  area.remove();
}

/* =====================================================================
   Message rows
   ===================================================================== */
function clearEmptyState() {
  const empty = document.getElementById("empty-state");
  if (empty) empty.remove();
}

function renderEmptyState() {
  // Rebuild on every call: the agent name may only become known
  // after /api/agents or the first snapshot arrives.
  const existing = document.getElementById("empty-state");
  if (existing) existing.remove();
  if (chatInner.childElementCount > 0) return;

  const box = el("div", "empty-state");
  box.id = "empty-state";
  const name = currentAgentName || "the agent";
  box.appendChild(el("p", "", "No messages yet"));
  chatInner.appendChild(box);
}

// Consecutive tool calls (and their results) share one collapsible block.
// A normal chat message closes the group, so the next call starts a fresh one.
let toolGroup = null;

function resetToolBlocks() {
  toolBlocks.clear();
  toolGroup = null;
  lastToolBlock = null;
}

function closeToolGroup() {
  toolGroup = null;
}

function makeToolBlock(name, callId) {
  const toolName = name || "tool";

  // Extend the active group instead of adding another row.
  if (toolGroup) {
    toolGroup.names.push(toolName);
    toolGroup.toolName.textContent = toolGroup.names.join(", ");
    toolGroup.pending++;
    if (callId) {
      toolBlocks.set(callId, toolGroup);
      toolGroup.callNames.set(callId, toolName);
    }
    lastToolBlock = toolGroup;
    return toolGroup;
  }

  clearEmptyState();

  const row = el("div", "msg-row tool");
  const details = el("details", "tool-details");
  const summary = el("summary", "tool-summary");
  summary.innerHTML = `<svg class="tool-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 18l6-6-6-6"/></svg>`;

  const label = el("span", "tool-label");
  label.append("called tool ");
  const nameEl = el("span", "tool-name");
  nameEl.textContent = toolName;
  label.appendChild(nameEl);

  const status = el("span", "tool-status");
  status.textContent = "pending";
  summary.append(label, status);

  const body = el("div", "tool-body");
  details.append(summary, body);
  row.appendChild(details);
  chatInner.appendChild(row);

  const block = {
    row, details, body, status,
    toolName: nameEl,
    names: [toolName],
    callNames: new Map(),
    calls: new Map(),
    callSeq: 0,
    seenResults: new Set(),
    pending: 1,
  };
  if (callId) {
    toolBlocks.set(callId, block);
    block.callNames.set(callId, toolName);
  }
  toolGroup = block;
  lastToolBlock = block;
  return block;
}

// Arguments and results of the same call live in one .tool-call-group. The
// dashed divider is inserted only when both are present, so an unfinished call
// does not show a dangling separator.
function addToolSection(block, base, content, toolName, callId) {
  const key = callId || ("#" + block.callSeq++);
  let call = block.calls.get(key);
  if (!call) {
    const node = el("div", "tool-call-group");
    block.body.appendChild(node);
    call = { node, hasArguments: false };
    block.calls.set(key, call);
  }

  if (base === "Result" && call.hasArguments) {
    const split = el("div", "tool-split");
    split.appendChild(el("span", "", "→"));
    call.node.appendChild(split);
  }

  const section = el("div", "tool-section");
  const label = el("div", "tool-section-label");
  const nameEl = el("span", "tool-call-label-name", toolName || "tool");
  label.append(nameEl, " " + base.toLowerCase());
  const pre = el("pre", "tool-pre");
  pre.textContent = content || "";
  section.append(label, pre);
  call.node.appendChild(section);
  if (base === "Arguments") call.hasArguments = true;
}

function makeMessage(role, type, timestamp) {
  // A regular message ends a run of tool calls; the next call starts a new card.
  closeToolGroup();
  clearEmptyState();
  const timeLabel = formatTime(timestamp);

  if (role === "error") {
    const row = el("div", "msg-error");
    chatInner.appendChild(row);
    return { row, body: row };
  }

  if (role === "user") {
    const row = el("div", "msg-row user");
    const bodyCol = el("div", "msg-body");
    const meta = el("div", "msg-meta");
    const author = el("span", "msg-author");
    author.textContent = "YOU";
    meta.appendChild(author);
    if (timeLabel) meta.appendChild(el("span", "msg-time", timeLabel));
    const bubble = el("div", "msg-bubble md-content");
    bodyCol.append(meta, bubble);
    row.appendChild(bodyCol);
    chatInner.appendChild(row);
    return { row, body: bubble };
  }

  const isReasoning = role === "reasoning";
  const row = el("div", "msg-row " + (isReasoning ? "reasoning" : "agent"));

  const bodyCol = el("div", "msg-body");
  const meta = el("div", "msg-meta");
  const author = el("span", "msg-author");
  author.textContent = isReasoning ? "Reasoning" : (currentAgentName || "Agent");
  meta.appendChild(author);
  if (timeLabel) meta.appendChild(el("span", "msg-time", timeLabel));
  const bubble = el("div", "msg-bubble md-content");
  bodyCol.append(meta, bubble);
  row.appendChild(bodyCol);
  chatInner.appendChild(row);
  return { row, body: bubble };
}

function entryText(entry) {
  if (entry.type !== 2 || !entry.reasoning) return entry.text || "";
  const r = entry.reasoning;
  return [...(r.summary || []), ...(r.content || [])].join("");
}

function frozenBubble(entry) {
  const text = entryText(entry);
  const images = entry.images || [];
  if (!text && images.length === 0 && entry.type !== 3 && entry.type !== 4) return null;

  if (entry.type === 3) { // tool call: add it to the current tool group
    const call = entry.tool_call || {};
    const name = call.name || "tool";
    const block = makeToolBlock(name, call.call_id);
    addToolSection(block, "Arguments", call.arguments || "", name, call.call_id);
    return block.row;
  }

  if (entry.type === 4) { // tool result: attach to the matching call block
    const result = entry.tool_result || {};
    let block = toolBlocks.get(result.call_id) || lastToolBlock;
    if (!block) block = makeToolBlock("result", result.call_id);
    const name = block.callNames.get(result.call_id) || "tool";
    addToolSection(block, "Result", result.output || "", name, result.call_id);
    if (!block.seenResults.has(result.call_id)) {
      block.seenResults.add(result.call_id);
      block.pending--;
    }
    if (block.pending <= 0) {
      block.status.textContent = "done";
      block.status.classList.add("done");
    }
    return block.row;
  }

  // Image tokens are the images of a user prompt, published separately so the
  // TUI can show them apart from the text — render them as user bubbles here.
  const role = entry.type === 5 ? "user" : (ENTRY[entry.type] || "unknown");
  const message = makeMessage(role, entry.type, entry.timestamp);

  switch (entry.type) {
    case 0: // user
      if (text) renderMarkdownInto(message.body, text);
      else message.body.remove();
      appendUserImages(message.row, images);
      break;
    case 1: // assistant
    case 2: // reasoning
      renderMarkdownInto(message.body, text);
      break;
    case 5: // image
      message.body.remove();
      appendUserImages(message.row, images);
      break;
    default:
      message.body.textContent = text;
  }
  return message.row;
}

/* =====================================================================
   Errors and command results (not stored history entries)
   ===================================================================== */
function showError(message) {
  closeToolGroup();
  const row = el("div", "msg-error");
  row.textContent = message || "Unknown error";
  chatInner.appendChild(row);
  scrollToBottom();
}

function appendCommandResult(text) {
  closeToolGroup();
  clearEmptyState();
  const row = el("div", "msg-row command");
  const col = el("div", "msg-body");
  const meta = el("div", "msg-meta");
  meta.append(
    el("span", "msg-author", "Command"),
    el("span", "msg-time", formatTime(Date.now()))
  );
  const bubble = el("div", "msg-bubble md-content");
  col.append(meta, bubble);
  row.append(col);
  chatInner.appendChild(row);

  // One single bubble with full markdown / math / code rendering.
  renderMarkdownInto(bubble, text || "");
  updateWordCount();
  scrollToBottom();
}
