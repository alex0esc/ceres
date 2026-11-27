"use strict";

/* =====================================================================
   Slash-command autocomplete for the composer.

   Mirrors the TUI popup: it walks the command tree for the token currently
   being typed, offers the children of the reached node, and stops once a leaf
   is reached (the rest are free-form arguments). The tree itself comes from
   GET /api/commands, so definitions live in exactly one place.
   ===================================================================== */

const CMD_MAX_ITEMS = 6;

// parseCommandInput mirrors internal/bubbletea.parseCommandInput. It splits the
// value into leading whitespace, completed command tokens and the token being
// completed. A trailing space means the current token is empty.
function parseCommandInput(value) {
  const trimmed = String(value || "").replace(/^\s+/, "");
  if (!trimmed.startsWith("/")) return null;

  const leading = value.slice(0, value.length - trimmed.length);
  const tokens = trimmed.split(/\s+/).filter(Boolean);
  if (tokens.length === 0) return null;

  const endsWithSpace = /\s$/.test(trimmed);
  return {
    leading,
    pathTokens: endsWithSpace ? tokens : tokens.slice(0, -1),
    partial: endsWithSpace ? "" : tokens[tokens.length - 1],
  };
}

class CommandAutocomplete {
  constructor(input, popup) {
    this.input = input;
    this.popup = popup;
    this.commands = [];
    this.items = [];
    this.index = 0;
    this.visible = false;
    this.lastNames = null;
  }

  setCommands(commands) {
    this.commands = Array.isArray(commands) ? commands : [];
    this.update();
  }

  // update recomputes suggestions for the current input value. The highlighted
  // index is kept while the suggestion set is unchanged, so typing more letters
  // of the same prefix does not jump back to the first entry.
  update() {
    const suggestions = this.suggest(this.input.value || "");
    const names = suggestions.map((s) => s.name).join("\u0000");
    const changed = names !== this.lastNames;
    this.lastNames = names;
    this.items = suggestions.slice(0, CMD_MAX_ITEMS);

    if (this.items.length === 0) {
      this.hide();
      return;
    }

    if (changed) this.index = 0;
    else this.index = Math.min(this.index, this.items.length - 1);
    this.visible = true;
    this.render();
  }

  hide() {
    this.visible = false;
    this.items = [];
    this.index = 0;
    this.lastNames = null;
    this.popup.replaceChildren();
    this.popup.classList.add("hidden");
  }

  move(delta) {
    if (!this.visible || this.items.length === 0) return false;
    this.index = (this.index + delta + this.items.length) % this.items.length;
    this.render();
    return true;
  }

  // accept writes the highlighted command into the input, replacing only the
  // token currently being completed. A trailing space is left so the next token
  // (a subcommand, or a free-form argument) can be typed right away.
  accept() {
    if (!this.visible || this.items.length === 0) return false;

    const parsed = parseCommandInput(this.input.value || "");
    if (!parsed) {
      this.hide();
      return false;
    }

    const prefix = parsed.pathTokens.length === 0
      ? "/"
      : parsed.pathTokens.join(" ") + " ";

    this.input.value = parsed.leading + prefix + this.items[this.index].name + " ";
    this.input.focus();
    this.input.setSelectionRange(this.input.value.length, this.input.value.length);
    this.hide();
    // Notify app.js so the composer resizes and the next suggestion set appears.
    this.input.dispatchEvent(new Event("input", { bubbles: true }));
    return true;
  }

  // suggest walks the completed tokens down the tree and returns the children
  // of the node reached. See internal/bubbletea.commandPopup.suggest.
  suggest(value) {
    const parsed = parseCommandInput(value);
    if (!parsed) return [];

    let candidates;
    let prefix;
    let top = false;

    if (parsed.pathTokens.length === 0) {
      if (!parsed.partial.startsWith("/")) return [];
      prefix = parsed.partial.slice(1).toLowerCase();
      candidates = this.commands;
      top = true;
    } else {
      let node = this.commands.find(
        (c) => c.name.toLowerCase() === parsed.pathTokens[0].replace(/^\//, "").toLowerCase()
      );
      if (!node) return [];

      for (const token of parsed.pathTokens.slice(1)) {
        node = (node.subcommands || []).find(
          (c) => c.name.toLowerCase() === token.toLowerCase()
        );
        if (!node) return [];
      }
      if (!node.subcommands || node.subcommands.length === 0) return [];

      candidates = node.subcommands;
      prefix = parsed.partial.toLowerCase();
    }

    return (candidates || [])
      .filter((c) => c.name.toLowerCase().startsWith(prefix))
      .map((c) => ({
        name: c.name,
        display: top ? "/" + c.name : c.name,
        description: c.description || "",
      }));
  }

  render() {
    this.popup.replaceChildren();
    this.items.forEach((item, i) => {
      const row = document.createElement("div");
      row.className = "cmd-item" + (i === this.index ? " active" : "");

      const name = document.createElement("span");
      name.className = "cmd-name";
      name.textContent = item.display;

      const desc = document.createElement("span");
      desc.className = "cmd-desc";
      desc.textContent = item.description;

      row.append(name, desc);
      // Keep the textarea focused while clicking a suggestion.
      row.addEventListener("mousedown", (event) => event.preventDefault());
      row.addEventListener("click", () => {
        this.index = i;
        this.accept();
      });
      this.popup.appendChild(row);
    });
    this.popup.classList.remove("hidden");
  }
}

window.CommandAutocomplete = CommandAutocomplete;
