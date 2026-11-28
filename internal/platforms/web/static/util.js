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

const iconPicker = document.getElementById("icon-picker-popover");
const iconPresetGrid = document.getElementById("icon-preset-grid");
const customIconInput = document.getElementById("custom-icon-input");

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

const ACCENTS = ["moon"];

// Agent icon presets (24x24, stroke style). "satellite" is the default icon.
// Explicit size attributes keep icons small during the brief moment before
// style.css applies; CSS still overrides them for the various icon contexts.
const svgIcon = (body) => `<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${body}</svg>`;

const AGENT_ICONS = {
  satellite: svgIcon(`<path d="M13 7L9 3 5 7l4 4"/><path d="M17 11l4 4-4 4-4-4"/><path d="M8 12l4 4 6-6-4-4z"/><path d="M16 8l3-3"/><path d="M9 21a6 6 0 00-6-6"/>`),
  moon: svgIcon(`<path d="M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z"/>`),
  orchestrator: svgIcon(`<circle cx="12" cy="5" r="2.5"/><circle cx="5" cy="19" r="2.5"/><circle cx="19" cy="19" r="2.5"/><path d="M10.5 7L6.5 16.5M13.5 7l4 9.5M7.5 19h9"/>`),
  code: svgIcon(`<path d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4"/>`),
  terminal: svgIcon(`<path d="M4 17l6-5-6-5M12 19h8"/>`),
  bot: svgIcon(`<rect x="4" y="8" width="16" height="11" rx="3"/><path d="M12 4v4M9 13h.01M15 13h.01M2 13h2M20 13h2"/>`),
  brain: svgIcon(`<path d="M9.5 3a3.5 3.5 0 00-3.2 4.9A3.5 3.5 0 005 14.5 3.5 3.5 0 008.5 18H11V3H9.5zM14.5 3a3.5 3.5 0 013.2 4.9A3.5 3.5 0 0119 14.5 3.5 3.5 0 0115.5 18H13V3h1.5z"/>`),
  database: svgIcon(`<ellipse cx="12" cy="5" rx="7" ry="3"/><path d="M5 5v14c0 1.66 3.13 3 7 3s7-1.34 7-3V5M5 12c0 1.66 3.13 3 7 3s7-1.34 7-3"/>`),
  globe: svgIcon(`<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 010 18M12 3a14 14 0 000 18"/>`),
  research: svgIcon(`<path d="M9 17v-6m4 6V7m4 10v-4M5 21h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v14a2 2 0 002 2z"/>`),
  search: svgIcon(`<path d="M21 21l-5.2-5.2M17 10.5a6.5 6.5 0 11-13 0 6.5 6.5 0 0113 0z"/>`),
  copywriter: svgIcon(`<path d="M15.232 5.232l3.536 3.536M9 11l6-6 3 3-6 6H9v-3zM5 19h14"/>`),
  security: svgIcon(`<path d="M12 3l7 3v6c0 4.97-3.05 8.85-7 10-3.95-1.15-7-5.03-7-10V6l7-3z"/>`),
  spark: svgIcon(`<path d="M12 3l1.9 5.1L19 10l-5.1 1.9L12 17l-1.9-5.1L5 10l5.1-1.9L12 3z"/>`),
  star: svgIcon(`<polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/>`),
  sun: svgIcon(`<circle cx="12" cy="12" r="5"/><path d="M12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42"/>`),
  zap: svgIcon(`<polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/>`),
  heart: svgIcon(`<path d="M20.84 4.61a5.5 5.5 0 00-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 00-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 000-7.78z"/>`),
  cpu: svgIcon(`<rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6"/><path d="M9 1v3M15 1v3M9 20v3M15 20v3M20 9h3M20 14h3M1 9h3M1 14h3"/>`),
  cloud: svgIcon(`<path d="M18 10h-1.26A8 8 0 109 20h9a5 5 0 000-10z"/>`),
  book: svgIcon(`<path d="M4 19.5A2.5 2.5 0 016.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 014 19.5v-15A2.5 2.5 0 016.5 2z"/>`),
  compass: svgIcon(`<circle cx="12" cy="12" r="10"/><polygon points="16.24 7.76 14.12 14.12 7.76 16.24 9.88 9.88 16.24 7.76"/>`),
  flag: svgIcon(`<path d="M4 15s1-1 4-1 5 2 8 2 4-1 4-1V3s-1 1-4 1-5-2-8-2-4 1-4 1z"/><path d="M4 22v-7"/>`),
  key: svgIcon(`<path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 11-7.78 7.78 5.5 5.5 0 017.78-7.78zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/>`),
  lock: svgIcon(`<rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0110 0v4"/>`),
  mail: svgIcon(`<path d="M4 4h16a2 2 0 012 2v12a2 2 0 01-2 2H4a2 2 0 01-2-2V6a2 2 0 012-2z"/><polyline points="22,6 12,13 2,6"/>`),
  calendar: svgIcon(`<rect x="3" y="4" width="18" height="18" rx="2"/><path d="M16 2v4M8 2v4M3 10h18"/>`),
  clock: svgIcon(`<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>`),
  camera: svgIcon(`<path d="M23 19a2 2 0 01-2 2H3a2 2 0 01-2-2V8a2 2 0 012-2h4l2-3h6l2 3h4a2 2 0 012 2z"/><circle cx="12" cy="13" r="4"/>`),
  music: svgIcon(`<path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/>`),
  image: svgIcon(`<rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="8.5" cy="8.5" r="1.5"/><path d="M21 15l-5-5L5 21"/>`),
  wrench: svgIcon(`<path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z"/>`),
  layers: svgIcon(`<polygon points="12 2 2 7 12 12 22 7 12 2"/><polyline points="2 17 12 22 22 17"/><polyline points="2 12 12 17 22 12"/>`),
  box: svgIcon(`<path d="M21 16V8a2 2 0 00-1-1.73l-7-4a2 2 0 00-2 0l-7 4A2 2 0 003 8v8a2 2 0 001 1.73l7 4a2 2 0 002 0l7-4A2 2 0 0021 16z"/><polyline points="3.27 6.96 12 12.01 20.73 6.96"/><line x1="12" y1="22.08" x2="12" y2="12"/>`),
  target: svgIcon(`<circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="6"/><circle cx="12" cy="12" r="2"/>`),
  gift: svgIcon(`<polyline points="20 12 20 22 4 22 4 12"/><rect x="2" y="7" width="20" height="5"/><line x1="12" y1="22" x2="12" y2="7"/><path d="M12 7H7.5a2.5 2.5 0 010-5C11 2 12 7 12 7zM12 7h4.5a2.5 2.5 0 000-5C13 2 12 7 12 7z"/>`),
  coffee: svgIcon(`<path d="M18 8h1a4 4 0 010 8h-1"/><path d="M2 8h16v9a4 4 0 01-4 4H6a4 4 0 01-4-4V8z"/><line x1="6" y1="1" x2="6" y2="4"/><line x1="10" y1="1" x2="10" y2="4"/><line x1="14" y1="1" x2="14" y2="4"/>`),
  anchor: svgIcon(`<circle cx="12" cy="5" r="3"/><line x1="12" y1="22" x2="12" y2="8"/><path d="M5 12H2a10 10 0 0020 0h-3"/>`),
  feather: svgIcon(`<path d="M20.24 12.24a6 6 0 00-8.49-8.49L5 10.5V19h8.5z"/><line x1="16" y1="8" x2="2" y2="22"/><line x1="17.5" y1="15" x2="9" y2="15"/>`),
  eye: svgIcon(`<path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/>`),
  pin: svgIcon(`<path d="M21 10c0 7-9 13-9 13S3 17 3 10a9 9 0 0118 0z"/><circle cx="12" cy="10" r="3"/>`),
  trend: svgIcon(`<polyline points="23 6 13.5 15.5 8.5 10.5 1 18"/><polyline points="17 6 23 6 23 12"/>`),
  activity: svgIcon(`<polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/>`),
  hash: svgIcon(`<line x1="4" y1="9" x2="20" y2="9"/><line x1="4" y1="15" x2="20" y2="15"/><line x1="10" y1="3" x2="8" y2="21"/><line x1="16" y1="3" x2="14" y2="21"/>`),
  smile: svgIcon(`<circle cx="12" cy="12" r="10"/><path d="M8 14s1.5 2 4 2 4-2 4-2"/><line x1="9" y1="9" x2="9.01" y2="9"/><line x1="15" y1="9" x2="15.01" y2="9"/>`),
  users: svgIcon(`<path d="M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 00-3-3.87M16 3.13a4 4 0 010 7.75"/>`),
  bell: svgIcon(`<path d="M18 8A6 6 0 006 8c0 7-3 9-3 9h18s-3-2-3-9M13.73 21a2 2 0 01-3.46 0"/>`),
  home: svgIcon(`<path d="M3 9l9-7 9 7v11a2 2 0 01-2 2H5a2 2 0 01-2-2z"/><polyline points="9 22 9 12 15 12 15 22"/>`),
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
  const button = document.getElementById("reasoning-toggle-btn");
  const state = document.getElementById("reasoning-state");
  button.classList.toggle("on", showReasoning);
  button.setAttribute("aria-pressed", String(showReasoning));
  state.textContent = showReasoning ? "On" : "Off";
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
  syncLabel.hidden = state === "connected";
  syncLabel.textContent =
    state === "connected" ? "" :
    state === "connecting" ? "connecting…" : "disconnected";
  syncPill.setAttribute("aria-label", "WebSocket " + state);
}

const ORBIT_SVG = '<svg class="orbit-icon" width="22" height="22" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="5.25" fill="none" stroke="currentColor" stroke-width="1" opacity=".45"/><g class="orbit-satellite-wrap"><circle cx="8" cy="2.75" r="1.5" fill="currentColor"/></g></svg>';

// Idempotent on purpose: it is called on every status poll, every send and
// every stream event. Rebuilding the icon or resetting the class on each call
// restarted the orbit animation, so the DOM is only touched when the state
// really changes. The icon itself is created exactly once.
function setWorkingState(next) {
  const valid = ["idle", "busy", "compressing"];
  const value = valid.includes(next) ? next : "idle";
  if (!stateVisual.firstElementChild) stateVisual.innerHTML = ORBIT_SVG;
  if (statePill.dataset.state === value) return;

  workingState = value;
  statePill.dataset.state = value;
  statePill.classList.remove("idle", "busy", "compressing");
  statePill.classList.add(value);
  stateLabel.textContent = value;

  const stopping = workingState !== "idle";
  sendButton.classList.toggle("is-stop", stopping);
  sendButton.setAttribute("aria-label", stopping ? "Stop agent" : "Send message");
  sendButton.title = stopping ? "Stop agent (Esc)" : "";
  if (stopping) sendButton.setAttribute("aria-keyshortcuts", "Escape"); else sendButton.removeAttribute("aria-keyshortcuts");
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

function isOverlayOpen() {
  const modal = document.getElementById("confirm-modal");
  return !iconPicker.classList.contains("hidden") ||
         !modal.classList.contains("hidden") ||
         sidebar.classList.contains("open");
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
