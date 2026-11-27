"use strict";

/* =====================================================================
   Everything image-related: picking and staging files in the composer,
   rendering them in chat, and the full-size viewer.

   Images travel as raw base64 (like Discord attachments) and are stored in
   history.Image, so the browser can rebuild a data URL at any time without
   asking the server. Depends on util.js for el/state and on render.js for
   showError; sendMessage is resolved at call time from connection.js.
   ===================================================================== */

const MAX_IMAGE_BYTES = 20 << 20; // 20 MiB per image

/* ---------- composer: pick, stage, remove ---------- */

// Only opens the native file picker; the change handler below does the work.
function openFilePicker() {
  const input = document.getElementById("file-input");
  if (input) input.click();
}

// A FileReader data URL is "data:<mime>;base64,<payload>". Strip the prefix so
// the server receives the same raw base64 string Discord sends for attachments.
function readImage(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const dataUrl = String(reader.result || "");
      const comma = dataUrl.indexOf(",");
      if (comma < 0) {
        reject(new Error("could not read image"));
        return;
      }
      const match = /^data:([^;]+);base64$/.exec(dataUrl.slice(0, comma));
      resolve({
        base64: dataUrl.slice(comma + 1),
        mime_type: match ? match[1] : file.type,
      });
    };
    reader.onerror = () => reject(new Error("could not read image"));
    reader.readAsDataURL(file);
  });
}

// Thumbnails of the images waiting in the composer, in selection order.
function renderStagedImages() {
  const container = document.getElementById("attachment-previews");
  if (!container) return;
  container.replaceChildren();
  stagedImages.forEach((img, index) => {
    const item = el("div", "attachment-preview");
    const thumb = el("img", "attachment-thumb");
    thumb.alt = "attachment";
    thumb.src = `data:${img.mime_type || "image/png"};base64,${img.base64}`;
    const remove = el("button", "attachment-remove");
    remove.type = "button";
    remove.title = "Remove image";
    remove.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M6 6l12 12M18 6L6 18"/></svg>`;
    remove.addEventListener("click", () => {
      if (sending) return;
      stagedImages.splice(index, 1);
      renderStagedImages();
    });
    item.append(thumb, remove);
    container.appendChild(item);
  });
  container.classList.toggle("hidden", stagedImages.length === 0);
}

const fileInput = document.getElementById("file-input");
fileInput.addEventListener("change", async () => {
  const files = Array.from(fileInput.files || []);
  fileInput.value = ""; // allow picking the same file again
  if (files.length === 0 || sending) return;

  for (const file of files) {
    if (!file.type.startsWith("image/")) {
      showError(`${file.name} is not an image.`);
      continue;
    }
    if (file.size > MAX_IMAGE_BYTES) {
      showError(`${file.name} is too large (max ${MAX_IMAGE_BYTES / (1024 * 1024)} MB).`);
      continue;
    }
    try {
      stagedImages.push(await readImage(file));
    } catch (err) {
      showError(`Could not read ${file.name}: ${err.message}`);
    }
  }
  renderStagedImages();
});

/* ---------- chat: thumbnails and full-size viewer ---------- */

function base64ToBlob(base64, mimeType) {
  const binary = window.atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return new Blob([bytes], { type: mimeType || "image/png" });
}

// Opens a small standalone viewer so the image can be seen full size and saved.
// A blob URL keeps the (possibly large) base64 out of the generated HTML.
function openImageViewer(img) {
  const mimeType = img.mime_type || "image/png";
  const blob = base64ToBlob(img.base64, mimeType);
  const url = URL.createObjectURL(blob);
  const viewer = window.open("", "_blank");
  if (!viewer) {
    URL.revokeObjectURL(url);
    showError("Could not open the image viewer. Allow popups for this site.");
    return;
  }

  const ext = (mimeType.split("/")[1] || "png").split(";")[0];
  const filename = "image." + ext;
  viewer.document.open();
  viewer.document.write(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>${filename}</title>
<style>
  html,body{margin:0;height:100%;background:#0b0d12;color:#e5e7eb;font-family:system-ui,sans-serif}
  body{display:flex;flex-direction:column;align-items:center;justify-content:center;gap:16px;padding:16px;box-sizing:border-box}
  img{max-width:100%;max-height:calc(100vh - 110px);object-fit:contain;border-radius:8px}
  .bar{display:flex;gap:10px}
  .bar a,.bar button{padding:8px 14px;border-radius:10px;border:1px solid #374151;background:#4f46e5;color:#fff;font:600 13px system-ui,sans-serif;text-decoration:none;cursor:pointer}
  .bar button{background:#1f2937}
</style></head><body>
  <img src="${url}" alt="Full size image">
  <div class="bar"><a href="${url}" download="${filename}">Download</a><button onclick="window.close()">Close</button></div>
</body></html>`);
  viewer.document.close();
  try { viewer.addEventListener("beforeunload", () => URL.revokeObjectURL(url)); }
  catch (err) { /* the blob URL dies with the opener */ }
}

function makeImage(img) {
  const node = el("img", "chat-image");
  node.alt = "attachment";
  node.src = `data:${img.mime_type || "image/png"};base64,${img.base64}`;
  node.addEventListener("click", () => openImageViewer(img));
  return node;
}

// Images belong to a user prompt but must not sit inside the indigo text
// bubble: put them first in the user column, before the text and the time.
function appendUserImages(row, images) {
  const col = row.querySelector(".user-col");
  if (!col || images.length === 0) return;
  const anchor = col.querySelector(".user-bubble") || col.querySelector(".user-time");
  const wrap = el("div", "user-images");
  for (const img of images) {
    const holder = el("div", "user-image");
    holder.appendChild(makeImage(img));
    wrap.appendChild(holder);
  }
  if (anchor) col.insertBefore(wrap, anchor);
  else col.appendChild(wrap);
}
