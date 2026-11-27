"use strict";

/* =====================================================================
   Shared DOM references, application state and small helpers.

   Loaded first. Classic scripts with defer are deliberate: index.html has
   inline onclick/oninput handlers that need these functions on window.
   ===================================================================== */

/* DOM references */
const chat = document.getElementById("chat");
const chatInner = document.getElementById("chat-messages-inner");
const composer = document.getElementById("composer");
const promptEl = document.getElementById("prompt");
const agentList = document.getElementById("agent-list");
const agentSearch = document.getElementById("agent-search");
const sendButton = document.getElementById("send-btn");
const currentAgentEl = document.getElementById("current-agent");
const headerDescEl = document.getElementById("header-agent-desc");
const headerAvatarEl = document.getElementById("header-agent-avatar");
const iconPicker = document.getElementById("icon-picker-popover");
const iconPresetGrid = document.getElementById("icon-preset-grid");
const customIconInput = document.getElementById("custom-icon-input");
const accentRow = document.getElementById("accent-preset-row");
const headerTokenCount = document.getElementById("header-token-count");
const tooltipWordCount = document.getElementById("tooltip-word-count");
const statePill = document.getElementById("working-indicator-pill");
const stateVisual = document.getElementById("working-indicator-visual");
const stateLabel = document.getElementById("working-indicator-state");
const sidebar = document.getElementById("left-sidebar");
const mobileBackdrop = document.getElementById("mobile-backdrop");
const syncPill = document.getElementById("sync-pill");
const syncLabel = document.getElementById("sync-label");
const cmdPopup = document.getElementById("cmd-popup");

/* Application state */
let selectedAgent = window.localStorage.getItem("ceres-agent") || "";
let currentAgentName = "";
let agents = [];
let visualOverrides = loadJSON("ceres-visuals", {});
let showReasoning = window.localStorage.getItem("ceres-reasoning") !== "false";
let ws = null;
let intentionalClose = false;
let sending = false;
const MAX_RECONNECT_ATTEMPTS = 3;
let reconnectAttempts = 0;
let workingState = "idle";
let commandAC = null;

// Images picked in the file dialog wait here until the message is sent.
let stagedImages = [];

// Streaming state (one message at a time).
let streamBubble = null;
let streamType = null;
let rawMarkdown = "";
let renderFrame = 0;

// Tool calls and their results belong to one collapsible block, linked by
// call_id. A result may arrive as a separate delta after the call.
const toolBlocks = new Map();
let lastToolBlock = null;

// history.Entry.Type values (see internal/history/entry.go)
const ENTRY = { 0: "user", 1: "assistant", 2: "reasoning", 3: "tool_call", 4: "tool_result", 5: "image" };
const TEXT_TYPES = new Set([0, 1, 2]);

const ACCENTS = ["indigo", "emerald", "cyan", "violet", "rose", "amber"];

const AGENT_ICONS = {
  orchestrator: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="5" r="2.5"/><circle cx="5" cy="19" r="2.5"/><circle cx="19" cy="19" r="2.5"/><path stroke-linecap="round" stroke-linejoin="round" d="M10.5 7L6.5 16.5M13.5 7l4 9.5M7.5 19h9"/></svg>`,
  code: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4"/></svg>`,
  terminal: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 17l6-5-6-5M12 19h8"/></svg>`,
  bot: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="4" y="8" width="16" height="11" rx="3"/><path stroke-linecap="round" stroke-linejoin="round" d="M12 4v4M9 13h.01M15 13h.01M2 13h2M20 13h2"/></svg>`,
  brain: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M9.5 3a3.5 3.5 0 00-3.2 4.9A3.5 3.5 0 005 14.5 3.5 3.5 0 008.5 18H11V3H9.5zM14.5 3a3.5 3.5 0 013.2 4.9A3.5 3.5 0 0119 14.5 3.5 3.5 0 0115.5 18H13V3h1.5z"/></svg>`,
  database: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><ellipse cx="12" cy="5" rx="7" ry="3"/><path stroke-linecap="round" stroke-linejoin="round" d="M5 5v14c0 1.66 3.13 3 7 3s7-1.34 7-3V5M5 12c0 1.66 3.13 3 7 3s7-1.34 7-3"/></svg>`,
  globe: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9"/><path stroke-linecap="round" stroke-linejoin="round" d="M3 12h18M12 3a14 14 0 010 18M12 3a14 14 0 000 18"/></svg>`,
  research: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M9 17v-6m4 6V7m4 10v-4M5 21h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v14a2 2 0 002 2z"/></svg>`,
  search: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-5.2-5.2M17 10.5a6.5 6.5 0 11-13 0 6.5 6.5 0 0113 0z"/></svg>`,
  copywriter: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M15.232 5.232l3.536 3.536M9 11l6-6 3 3-6 6H9v-3zM5 19h14"/></svg>`,
  security: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 3l7 3v6c0 4.97-3.05 8.85-7 10-3.95-1.15-7-5.03-7-10V6l7-3z"/></svg>`,
  spark: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 3l1.9 5.1L19 10l-5.1 1.9L12 17l-1.9-5.1L5 10l5.1-1.9L12 3z"/></svg>`,
};

/* =====================================================================
   Small helpers
   ===================================================================== */
function loadJSON(key, fallback) {
  try {
    return JSON.parse(window.localStorage.getItem(key)) || fallback;
  } catch (err) {
    return fallback;
  }
}

function saveJSON(key, value) {
  try {
    window.localStorage.setItem(key, JSON.stringify(value));
  } catch (err) {
    /* storage is optional */
  }
}

function escapeHtml(str) {
  return String(str == null ? "" : str)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function formatCount(num) {
  const n = Number(num) || 0;
  if (n >= 10000) return (n / 1000).toFixed(1) + "k";
  return n.toLocaleString("en-US");
}

// Stored history entries carry a Unix-ms timestamp stamped in history.Push().
// Live messages have none, so they get the local time once their stream ends
// (see finalizeStream).
function formatTime(timestamp) {
  if (!timestamp) return "";
  return new Date(timestamp).toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit" });
}

// Adds a time label to a message row that came from a live delta. Frozen
// entries already have one from their stored timestamp; this is only for rows
// created by startStream, where the server had no timestamp to send.
function stampMessageTime(row, timestamp) {
  const label = formatTime(timestamp);
  if (!row || !label || row.querySelector(".user-time, .msg-time")) return;
  const userCol = row.querySelector(".user-col");
  if (userCol) {
    userCol.appendChild(el("div", "user-time", label));
    return;
  }
  const meta = row.querySelector(".msg-meta");
  if (meta) meta.appendChild(el("span", "msg-time", label));
}

function hashString(text) {
  let hash = 0;
  for (let i = 0; i < text.length; i++) hash = (hash * 31 + text.charCodeAt(i)) >>> 0;
  return hash;
}

function getMonogram(name) {
  const cleaned = String(name || "").replace(/[^a-zA-Z0-9ÄÖÜäöüß\s-]/g, " ").trim();
  if (!cleaned) return "AI";
  const words = cleaned.split(/[\s-]+/).filter(Boolean);
  return words.length >= 2
    ? (words[0][0] + words[1][0]).toUpperCase()
    : cleaned.slice(0, 2).toUpperCase();
}

/* =====================================================================
   UI primitives (shared state + small DOM updates)
   ===================================================================== */
function updateReasoningToggle() {
  const state = document.getElementById("reasoning-state");
  state.textContent = showReasoning ? "On" : "Off";
  state.className = "toggle-state mono " + (showReasoning ? "on" : "off");
  chatInner.classList.toggle("hide-reasoning", !showReasoning);
}

function toggleReasoning() {
  showReasoning = !showReasoning;
  try { window.localStorage.setItem("ceres-reasoning", String(showReasoning)); } catch (err) { /* optional */ }
  updateReasoningToggle();
  // Entries hidden at render time were never added to the DOM, so toggling
  // visibility needs a fresh snapshot instead of a CSS-only change.
  reloadChat();
}

function toggleLeftDrawer() {
  const open = !sidebar.classList.contains("open");
  sidebar.classList.toggle("open", open);
  mobileBackdrop.classList.toggle("show", open);
}

function closeLeftDrawer() {
  sidebar.classList.remove("open");
  mobileBackdrop.classList.remove("show");
}

function setConnectionState(state) {
  syncPill.className = "sync-pill " + state;
  syncLabel.textContent =
    state === "connected" ? "connected" :
    state === "connecting" ? "connecting…" : "disconnected";
  if (state !== "disconnected") syncPill.title = "WebSocket connection status";
}

function setWorkingState(next) {
  const valid = ["idle", "busy", "compressing"];
  workingState = valid.includes(next) ? next : "idle";
  statePill.className = "state-pill " + workingState;
  stateLabel.textContent = workingState;

  if (workingState === "idle") {
    stateVisual.innerHTML = `<span class="idle-dot"></span>`;
  } else if (workingState === "busy") {
    stateVisual.innerHTML = `<span class="busy-bars"><span></span><span></span><span></span><span></span></span>`;
  } else {
    stateVisual.innerHTML = `
      <span class="compress-visual">
        <svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" class="compress-arrow-left"><path d="M4 2l4 4-4 4"/></svg>
        <span class="compress-core"></span>
        <svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" class="compress-arrow-right"><path d="M8 2L4 6l4 4"/></svg>
      </span>`;
  }
}

function setTokenCount(tokens) {
  headerTokenCount.textContent = formatCount(tokens);
}

// Word count is a client-side convenience metric. It is deliberately skipped
// while a message is streaming so the DOM is never scanned for every token.
function updateWordCount() {
  if (streamBubble) return;
  let words = 0;
  for (const row of chatInner.children) {
    if (row.id === "empty-state") continue;
    if (!showReasoning && row.classList.contains("reasoning")) continue;
    const text = row.textContent || "";
    const matches = text.match(/[\p{L}\p{N}]+(?:['’\-][\p{L}\p{N}]+)*/gu);
    if (matches) words += matches.length;
  }
  tooltipWordCount.textContent = words.toLocaleString("en-US");
}

/* =====================================================================
   Confirmation modal + toasts
   ===================================================================== */
let pendingConfirm = null;

function promptResetCurrentChat() {
  const name = currentAgentName || "this agent";
  openConfirmModal(
    `Clear history with ${name}?`,
    "All messages and the token count of this session will be reset.",
    async () => {
      try {
        await postText("/clear", [], false);
      } catch (err) {
        showError(`Could not clear history: ${err.message}`);
      }
    }
  );
}

function openConfirmModal(title, desc, onConfirm) {
  document.getElementById("confirm-modal-title").textContent = title;
  document.getElementById("confirm-modal-desc").textContent = desc;
  pendingConfirm = onConfirm;
  const modal = document.getElementById("confirm-modal");
  modal.classList.remove("hidden");
  modal.classList.add("flex");
  document.getElementById("confirm-modal-action").onclick = () => {
    const callback = pendingConfirm;
    closeConfirmModal();
    if (typeof callback === "function") callback();
  };
}

function closeConfirmModal() {
  const modal = document.getElementById("confirm-modal");
  modal.classList.add("hidden");
  modal.classList.remove("flex");
  pendingConfirm = null;
}

function showToast(message) {
  const container = document.getElementById("toast-container");
  const toast = el("div", "toast");
  toast.textContent = message;
  container.appendChild(toast);
  window.setTimeout(() => {
    toast.style.opacity = "0";
    toast.style.transition = "opacity 0.25s ease";
    window.setTimeout(() => toast.remove(), 260);
  }, 2400);
}
