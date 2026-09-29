// Shared surface styling for the extension: the in-page chooser menu and the
// toolbar popup draw from the same Kumo tokens as the vault. One stylesheet
// string, one row builder — the two surfaces stay visually identical.
(function () {
  const root = typeof window !== "undefined" ? window : globalThis;

  // Kumo values (apps/vault src/styles.css → @cloudflare/kumo). light-dark()
  // needs color-scheme on the owning root; both set on :host/.v-root below.
  const css = `
:host, .v-root {
  color-scheme: light dark;
  --v-base: light-dark(#ffffff, oklch(17% 0 0));
  --v-overlay: light-dark(oklch(97.5% 0 0), oklch(26.9% 0 0));
  --v-hairline: light-dark(oklch(93.5% 0 0), oklch(32% 0 0));
  --v-line: light-dark(oklch(14.5% 0 0 / 0.10), oklch(100% 0 0 / 0.09));
  --v-fill: light-dark(oklch(92.5% 0 0), oklch(30% 0 0));
  --v-fill-hover: light-dark(oklch(96% 0 0), oklch(24.5% 0 0));
  --v-text: light-dark(oklch(13% 0 0), oklch(96% 0 0));
  --v-subtle: light-dark(oklch(48% 0 0), oklch(70% 0 0));
  --v-brand: light-dark(oklch(0.5772 0.2324 260), oklch(0.72 0.16 262));
  --v-edge: light-dark(oklch(0% 0 0 / 0.12), oklch(100% 0 0 / 0.10));
  --v-drop: light-dark(oklch(0% 0 0 / 0.10), oklch(0% 0 0 / 0.45));
}
.v-menu {
  box-sizing: border-box;
  background: var(--v-base);
  border: 1px solid var(--v-line);
  border-radius: 10px;
  padding: 4px;
  box-shadow: 0 0 0 0.5px var(--v-edge), 0 4px 12px var(--v-drop), 0 16px 40px var(--v-drop);
  font: 13px/1.4 -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
  color: var(--v-text);
  -webkit-font-smoothing: antialiased;
}
.v-menu.v-field {
  padding: 3px;
  border-radius: 8px;
}
.v-row {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 6px 8px;
  border-radius: 7px;
  cursor: pointer;
  user-select: none;
}
.v-row:hover { background: var(--v-fill-hover); }
.v-row[data-active] { background: var(--v-fill); }
/* Slim rows — the field-attached suggestion reads like the browser's own
   autofill line: bare glyph, name and detail on one line, no card chrome. */
.v-row.v-slim {
  padding: 5px 8px;
  gap: 8px;
  border-radius: 6px;
}
.v-row.v-slim .v-tile {
  width: 15px;
  height: 15px;
  border: 0;
  background: none;
  border-radius: 0;
}
.v-row.v-slim .v-txt {
  display: flex;
  align-items: baseline;
  gap: 7px;
}
.v-row.v-slim .v-name {
  flex: none;
  max-width: 60%;
  font-size: 13px;
}
.v-row.v-slim .v-sub {
  flex: 1;
  min-width: 0;
  margin-top: 0;
}
button.v-row {
  appearance: none;
  -webkit-appearance: none;
  border: 0;
  background: none;
  font: inherit;
  color: inherit;
  text-align: left;
  width: 100%;
  margin: 0;
}
.v-tile {
  flex: none;
  width: 26px;
  height: 26px;
  border-radius: 7px;
  background: var(--v-overlay);
  border: 1px solid var(--v-line);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--v-subtle);
}
.v-row[data-kind="generate"] .v-tile { color: var(--v-brand); }
.v-txt { min-width: 0; flex: 1; }
.v-name {
  font-weight: 500;
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.v-sub {
  font-size: 12px;
  color: var(--v-subtle);
  margin-top: 1px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.v-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 10px 7px;
  border-bottom: 1px solid var(--v-line);
  margin-bottom: 4px;
}
.v-mark {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
  font-size: 12px;
  letter-spacing: 0.02em;
}
.v-mark svg { color: var(--v-brand); }
.v-where { font-size: 12px; color: var(--v-subtle); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.v-empty, .v-err {
  padding: 14px 12px;
  color: var(--v-subtle);
  font-size: 13px;
  text-align: center;
}
.v-err { color: var(--v-text); }
.v-sep { height: 1px; background: var(--v-line); margin: 4px 6px; }
/* The in-field mark — quiet until hovered, the way back in after dismissal. */
.v-field-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  padding: 0;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--v-subtle);
  cursor: pointer;
  font: inherit;
}
.v-field-icon:hover { color: var(--v-text); background: var(--v-fill-hover); }
.v-field-icon svg { display: block; }
`;

  // Stroke glyphs, 24 viewBox — inherit color via currentColor.
  const glyphs = {
    login:
      '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="15.5" r="4.5"/><path d="M11.2 12.3 20 3.5M15.5 8l3 3M18 5.5l2.5 2.5"/></svg>',
    card:
      '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="2.5" y="5" width="19" height="14" rx="2.5"/><path d="M2.5 9.5h19M6 15h4"/></svg>',
    identity:
      '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="2.5" y="4" width="19" height="16" rx="2.5"/><circle cx="8.5" cy="10.5" r="2"/><path d="M5.5 17c.6-1.8 1.7-2.6 3-2.6s2.4.8 3 2.6M14 9.5h5M14 13.5h5"/></svg>',
    generate:
      '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M12 4v4M12 16v4M4 12h4M16 12h4M7.8 7.8l2 2M14.2 14.2l2 2M16.2 7.8l-2 2M9.8 14.2l-2 2"/></svg>',
    save:
      '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="8.5"/><path d="M12 8.5v7M8.5 12h7"/></svg>',
    update:
      '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M20 11.5A8 8 0 1 0 20.8 14"/><path d="M20 4.5v7h-7"/></svg>',
    dismiss:
      '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M7 7l10 10M17 7L7 17"/></svg>',
    veil:
      '<svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor"><path fill-rule="evenodd" d="M8.64 12.24V7.2a3.36 4.32 0 0 1 6.72 0v5.04h-1.68V7.68a1.68 2.88 0 0 0-3.36 0v4.56zM7.68 11.76h8.64q2.4 0 2.52 1.92l0.36 5.04q0.12 2.16-2.04 2.4H6.84q-2.16-0.24-2.04-2.4l0.36-5.04q0.12-1.92 2.52-1.92zM9.4 15.6a1 1 0 0 1 2 0v0.8a1 1 0 0 1-2 0zM12.6 15.6a1 1 0 0 1 2 0v0.8a1 1 0 0 1-2 0z"/></svg>',
  };

  function el(tag, cls, text) {
    const d = document.createElement(tag);
    if (cls) {
      d.className = cls;
    }
    if (text !== undefined) {
      d.textContent = text;
    }
    return d;
  }

  // One chooser row: kind tile, name, secondary line. Callers attach their own
  // activation listener (content uses mousedown+preventDefault so the field
  // keeps focus; the popup uses click on a real button for keyboard reach).
  function entryRow(opts, tag) {
    const row = el(tag || "div", "v-row");
    if (tag === "button") {
      row.type = "button";
    }
    if (opts.slim) {
      row.classList.add("v-slim");
    }
    row.setAttribute("role", "option");
    if (opts.kind) {
      row.dataset.kind = opts.kind;
    }
    const tile = el("div", "v-tile");
    tile.innerHTML = glyphs[opts.kind] || glyphs.login;
    row.appendChild(tile);
    const txt = el("div", "v-txt");
    txt.appendChild(el("div", "v-name", opts.name || "item"));
    if (opts.sub) {
      txt.appendChild(el("div", "v-sub", opts.sub));
    }
    row.appendChild(txt);
    return row;
  }

  function setActive(rows, i) {
    rows.forEach(function (row, j) {
      if (j === i) {
        row.dataset.active = "";
      } else {
        row.removeAttribute("data-active");
      }
    });
  }

  root.veilUI = { css: css, glyphs: glyphs, entryRow: entryRow, setActive: setActive, el: el };
  if (typeof module !== "undefined" && module.exports) {
    module.exports = root.veilUI;
  }
})();
