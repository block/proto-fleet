package main

const indexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>MQTT Curtailment Simulator</title>
  <style>
    :root {
      color-scheme: light;
      --base-black: 0 0 0;
      --base-white: 255 255 255;
      --base-gray-2: 250 250 250;
      --base-gray-5: 242 242 242;
      --base-gray-10: 224 224 224;
      --base-orange: 254 124 0;
      --base-red: 250 43 55;
      --base-green: 56 166 0;
      --base-yellow: 253 138 0;
      --base-text-critical: 116 20 13;
      --base-text-success: 6 63 37;
      --base-text-warning: 135 73 0;

      --surface-base: rgb(var(--base-white));
      --surface-2: rgb(var(--base-gray-2));
      --surface-5: rgb(var(--base-gray-5));
      --text-primary: rgb(var(--base-black) / 90%);
      --text-primary-70: rgb(var(--base-black) / 70%);
      --text-primary-50: rgb(var(--base-black) / 50%);
      --text-contrast: rgb(var(--base-white));
      --border-5: rgb(var(--base-black) / 5%);
      --border-10: rgb(var(--base-black) / 10%);
      --border-20: rgb(var(--base-black) / 20%);
      --core-primary: rgb(var(--base-black) / 90%);
      --core-primary-5: rgb(var(--base-black) / 5%);
      --core-primary-10: rgb(var(--base-black) / 10%);
      --core-accent: rgb(var(--base-orange));
      --core-accent-10: rgb(var(--base-orange) / 10%);
      --intent-critical: rgb(var(--base-red));
      --intent-critical-10: rgb(var(--base-red) / 10%);
      --intent-critical-20: rgb(var(--base-red) / 20%);
      --intent-critical-text: rgb(var(--base-text-critical));
      --intent-success-10: rgb(var(--base-green) / 10%);
      --intent-success-text: rgb(var(--base-text-success));
      --intent-warning-10: rgb(var(--base-yellow) / 10%);
      --intent-warning-text: rgb(var(--base-text-warning));
      --shadow-50: 0 0 1px 0 rgb(var(--base-black) / 37%);

      --radius-xl: 12px;
      --radius-lg: 8px;
      --font-body: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      --font-mono: JetBrainsMono, ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    }
    * {
      box-sizing: border-box;
    }
    body {
      margin: 0;
      font-family: var(--font-body);
      background: var(--surface-base);
      color: var(--text-primary);
    }
    main {
      width: min(1160px, calc(100vw - 48px));
      margin: 40px auto;
      display: grid;
      gap: 24px;
    }
    header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 16px;
      padding-bottom: 8px;
    }
    .header-actions {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      justify-content: flex-end;
      gap: 10px;
    }
    h1 {
      margin: 0;
      font-size: 28px;
      font-weight: 500;
      line-height: 40px;
      letter-spacing: 0;
    }
    h2 {
      margin: 0 0 16px;
      color: var(--text-primary);
      font-size: 20px;
      font-weight: 500;
      line-height: 28px;
      letter-spacing: 0;
    }
    p {
      margin: 6px 0 0;
      color: var(--text-primary-70);
      font-size: 16px;
      line-height: 24px;
      letter-spacing: 0;
    }
    .grid {
      display: grid;
      grid-template-columns: minmax(0, 1fr) minmax(360px, 420px);
      gap: 16px;
      align-items: start;
    }
    .main-stack,
    .side {
      display: grid;
      gap: 16px;
      align-content: start;
    }
    .panel {
      background: var(--surface-base);
      border: 1px solid var(--border-5);
      border-radius: var(--radius-xl);
      padding: 24px;
    }
    .panel-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
      margin-bottom: 16px;
    }
    .panel-header h2 {
      margin: 0;
    }
    .fields {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 16px;
    }
    label {
      display: grid;
      gap: 6px;
      color: var(--text-primary-70);
      font-size: 12px;
      font-weight: 500;
      line-height: 20px;
      letter-spacing: 0;
    }
    input, textarea {
      width: 100%;
      border: 1px solid var(--border-5);
      border-radius: var(--radius-lg);
      padding: 12px 16px;
      font: inherit;
      font-size: 14px;
      line-height: 24px;
      background: var(--surface-base);
      color: var(--text-primary);
      outline: none;
      transition: border-color 160ms ease, box-shadow 160ms ease, background-color 160ms ease;
    }
    input {
      min-height: 56px;
    }
    input:focus, textarea:focus {
      border-color: var(--border-20);
      box-shadow: 0 0 0 4px var(--core-primary-5);
    }
    textarea {
      min-height: 104px;
      resize: vertical;
      font-family: var(--font-mono);
      font-size: 13px;
    }
    .checks {
      display: flex;
      flex-wrap: wrap;
      gap: 12px;
      margin-top: 16px;
    }
    .check {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      color: var(--text-primary);
      font-size: 14px;
      font-weight: 400;
      line-height: 24px;
    }
    .check input {
      min-height: 0;
      width: 16px;
      height: 16px;
      accent-color: var(--core-accent);
    }
    .actions {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      margin-top: 20px;
    }
    button, .button-link {
      border: 0;
      background: var(--core-primary-5);
      color: var(--text-primary);
      border-radius: 999px;
      padding: 8px 12px;
      font: inherit;
      font-size: 14px;
      font-weight: 500;
      line-height: 24px;
      letter-spacing: 0;
      cursor: pointer;
      transition: opacity 160ms ease, background-color 160ms ease, color 160ms ease;
      text-decoration: none;
    }
    button:hover, .button-link:hover {
      opacity: 0.8;
    }
    button:focus-visible, .button-link:focus-visible {
      outline: 2px solid var(--core-primary);
      outline-offset: 2px;
    }
    button.primary {
      background: var(--core-primary);
      color: var(--text-contrast);
    }
    button.danger {
      background: var(--intent-critical);
      color: var(--text-contrast);
    }
    button:disabled {
      cursor: not-allowed;
      opacity: 0.4;
    }
    .status {
      display: grid;
      gap: 0;
      font-size: 14px;
    }
    .row {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      padding: 12px 0;
      border-bottom: 1px solid var(--border-5);
    }
    .row:first-child {
      padding-top: 0;
    }
    .row:last-child {
      border-bottom: 0;
      padding-bottom: 0;
    }
    .copy-row {
      display: grid;
      grid-template-columns: minmax(92px, 0.55fr) minmax(0, 1fr) auto;
      align-items: center;
      gap: 10px;
      padding: 10px 0;
      border-bottom: 1px solid var(--border-5);
    }
    .copy-row:last-child {
      border-bottom: 0;
      padding-bottom: 0;
    }
    .key {
      color: var(--text-primary-50);
      font-size: 12px;
      line-height: 20px;
    }
    .value {
      text-align: right;
      color: var(--text-primary);
      font-weight: 500;
      overflow-wrap: anywhere;
    }
    .copy-value {
      min-width: 0;
      color: var(--text-primary);
      font-family: var(--font-mono);
      font-size: 12px;
      line-height: 20px;
      overflow-wrap: anywhere;
    }
    .copy-button {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 36px;
      height: 36px;
      padding: 0;
      flex: 0 0 auto;
    }
    .copy-button svg {
      width: 16px;
      height: 16px;
      stroke: currentColor;
      stroke-width: 2;
      fill: none;
      stroke-linecap: round;
      stroke-linejoin: round;
    }
    .copy-all {
      flex-shrink: 0;
    }
    .panel-footer {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      border-top: 1px solid var(--border-5);
      margin-top: 16px;
      padding-top: 16px;
    }
    .button-link[hidden] {
      display: none;
    }
    .pill {
      display: inline-flex;
      align-items: center;
      min-height: 24px;
      padding: 2px 8px;
      border-radius: 999px;
      background: var(--intent-success-10);
      color: var(--intent-success-text);
      font-size: 12px;
      font-weight: 500;
    }
    .pill.off {
      background: var(--intent-critical-10);
      color: var(--intent-critical-text);
    }
    .hint {
      border-top: 1px solid var(--border-5);
      margin-top: 16px;
      padding-top: 14px;
      color: var(--text-primary-70);
      font-size: 12px;
      line-height: 20px;
      letter-spacing: 0;
    }
    code {
      font-family: var(--font-mono);
      color: var(--text-primary);
      background: var(--core-primary-5);
      border-radius: 6px;
      padding: 1px 5px;
    }
    pre {
      margin: 0;
      white-space: pre-wrap;
      word-break: break-word;
      font-family: var(--font-mono);
      font-size: 12px;
      line-height: 1.5;
    }
    .log {
      display: grid;
      max-height: 360px;
      overflow: auto;
      border-top: 1px solid var(--border-5);
    }
    .log-entry {
      border-bottom: 1px solid var(--border-5);
      padding: 12px 0;
      background: var(--surface-base);
    }
    .log-entry:last-child {
      border-bottom: 0;
    }
    .log-entry.error {
      color: var(--intent-critical-text);
    }
    .log-time {
      display: block;
      color: var(--text-primary-50);
      font-size: 12px;
      line-height: 20px;
      margin-bottom: 4px;
    }
    @media (max-width: 860px) {
      main {
        width: min(100vw - 32px, 1160px);
        margin: 24px auto;
      }
      .grid, .fields {
        grid-template-columns: 1fr;
      }
      .copy-row {
        grid-template-columns: minmax(0, 1fr) auto;
      }
      .copy-row .key {
        grid-column: 1 / -1;
      }
      header {
        display: grid;
      }
      .header-actions {
        justify-content: flex-start;
      }
      .value {
        text-align: left;
      }
      .row {
        display: grid;
      }
    }
  </style>
</head>
<body>
  <main>
    <header>
      <div>
        <h1>MQTT Curtailment Simulator</h1>
        <p>Publish MaestroOS-compatible ON and OFF targets over MQTT for Proto Fleet testing.</p>
      </div>
      <div class="header-actions">
        <button id="refresh" type="button">Refresh</button>
      </div>
    </header>

    <div class="grid">
      <div class="main-stack">
        <section class="panel">
          <h2>Signal Controls</h2>
          <div class="fields">
            <label>Topic
              <input id="topic" value="maestro/target">
            </label>
            <label>Loop interval seconds
              <input id="interval" type="number" min="1" value="30">
            </label>
            <label>Timestamp offset seconds
              <input id="offset" type="number" value="0">
            </label>
            <label>Custom payload
              <textarea id="custom"></textarea>
            </label>
          </div>

          <div class="checks">
            <label class="check"><input id="retain" type="checkbox" checked>Retain payload</label>
            <label class="check"><input id="primary" type="checkbox" checked>Primary broker</label>
            <label class="check"><input id="secondary" type="checkbox" checked>Secondary broker</label>
          </div>

          <div class="actions">
            <button class="primary" type="button" data-action="publish" data-target="ON">Send ON Once</button>
            <button class="danger" type="button" data-action="publish" data-target="OFF">Send OFF Once</button>
            <button class="primary" type="button" data-action="loop" data-target="ON">Start ON Loop</button>
            <button class="danger" type="button" data-action="loop" data-target="OFF">Start OFF Loop</button>
            <button type="button" data-action="custom">Send Custom Once</button>
            <button type="button" data-action="clear">Clear Retained</button>
            <button type="button" data-action="stop">Stop Loop</button>
          </div>

          <div class="hint">
            Standard ON sends <code>{"target":100,"timestamp":now}</code>. Standard OFF sends
            <code>{"target":0,"timestamp":now}</code>. Loop mode publishes every 30 seconds by default.
          </div>
        </section>

        <section class="panel">
          <h2>Activity</h2>
          <div id="logs" class="log"></div>
        </section>
      </div>

      <div class="side">
        <section class="panel">
          <div class="panel-header">
            <h2>Connection Details</h2>
            <button class="copy-all" id="copy-all" type="button">Copy All</button>
          </div>
          <div id="connection-details"></div>
          <div class="panel-footer" id="settings-footer" hidden>
            <a class="button-link" id="settings-link" target="_blank" rel="noopener noreferrer" hidden>
              Open Curtailment Settings
            </a>
          </div>
        </section>

        <section class="panel">
          <h2>Status</h2>
          <div id="status" class="status"></div>
        </section>
      </div>
    </div>
  </main>

  <script>
    const state = {
      busy: false,
      formDirty: false,
      formHydrated: false
    };

    const fields = {
      topic: document.getElementById("topic"),
      interval: document.getElementById("interval"),
      offset: document.getElementById("offset"),
      custom: document.getElementById("custom"),
      retain: document.getElementById("retain"),
      primary: document.getElementById("primary"),
      secondary: document.getElementById("secondary"),
      connectionDetails: document.getElementById("connection-details"),
      settingsFooter: document.getElementById("settings-footer"),
      settingsLink: document.getElementById("settings-link"),
      status: document.getElementById("status"),
      logs: document.getElementById("logs")
    };

    const connectionDefaults = [
      ["Broker host 1", "192.168.2.240"],
      ["Broker host 2", "192.168.2.241"],
      ["Port", "1883"],
      ["Transport", "tcp"],
      ["Topic", "maestro/target"],
      ["Username", "proto-fleet"],
      ["Password", "proto-fleet"]
    ];

    fields.custom.placeholder = JSON.stringify({
      target: 0,
      timestamp: Math.floor(Date.now() / 1000)
    });

    renderConnectionDetails(connectionDefaults);
    watchFormEdits();

    document.querySelectorAll("[data-action]").forEach((button) => {
      button.addEventListener("click", async () => {
        const action = button.dataset.action;
        const target = button.dataset.target;
        if (action === "publish") await publish(target);
        if (action === "loop") await startLoop(target);
        if (action === "custom") await publishCustom();
        if (action === "clear") await clearRetained();
        if (action === "stop") await stopLoop();
      });
    });
    document.getElementById("refresh").addEventListener("click", refresh);
    document.getElementById("copy-all").addEventListener("click", async (event) => {
      await copyText(connectionDefaults.map(([key, value]) => key + ": " + value).join("\n"), event.currentTarget);
    });
    fields.connectionDetails.addEventListener("click", async (event) => {
      const button = event.target.closest("[data-copy]");
      if (!button) return;
      await copyText(button.dataset.copy, button);
    });

    function basePayload() {
      return {
        topic: fields.topic.value.trim(),
        retain: fields.retain.checked,
        primary_enabled: fields.primary.checked,
        secondary_enabled: fields.secondary.checked,
        timestamp_offset_seconds: Number(fields.offset.value || 0)
      };
    }

    async function publish(target) {
      await post("/api/publish", { ...basePayload(), target });
    }

    async function publishCustom() {
      await post("/api/publish", {
        ...basePayload(),
        target: "ON",
        custom_payload: fields.custom.value
      });
    }

    async function startLoop(target) {
      await post("/api/loop/start", {
        ...basePayload(),
        target,
        interval_seconds: Number(fields.interval.value || 30)
      });
    }

    async function stopLoop() {
      await post("/api/loop/stop", {});
    }

    async function clearRetained() {
      await post("/api/clear", {
        topic: fields.topic.value.trim(),
        primary_enabled: fields.primary.checked,
        secondary_enabled: fields.secondary.checked
      });
    }

    async function post(path, body) {
      setBusy(true);
      try {
        const response = await fetch(path, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body)
        });
        const payload = await response.json();
        if (!response.ok) throw new Error(payload.error || "request failed");
        state.formDirty = false;
        render(payload, { syncForm: true });
      } catch (error) {
        renderError(error);
      } finally {
        setBusy(false);
      }
    }

    async function refresh() {
      try {
        const response = await fetch("/api/status");
        render(await response.json(), { syncForm: !state.formHydrated });
      } catch (error) {
        renderError(error);
      }
    }

    function render(payload, options = {}) {
      updateSettingsLink(payload.links && payload.links.curtailment_settings_url);
      const s = payload.state || {};
      if (options.syncForm && !state.formDirty) {
        hydrateForm(s);
      }
      fields.status.innerHTML = [
        row("Loop", s.running ? "Running" : "Stopped"),
        row("Target", pill(s.target || "UNKNOWN")),
        row("Topic", escapeHTML(s.topic || "")),
        row("Interval", String(s.interval_seconds || 0) + "s"),
        row("Last publish", formatDate(s.last_published_at)),
        row("Messages", String(s.published_messages || 0)),
        row("Last error", escapeHTML(s.last_error || "-")),
        row("Last payload", "<pre>" + escapeHTML(s.last_payload || "-") + "</pre>")
      ].join("");
      fields.logs.innerHTML = (payload.logs || []).map((entry) =>
        "<div class=\"log-entry " + (entry.level === "error" ? "error" : "") + "\">" +
          "<span class=\"log-time\">" + new Date(entry.time).toLocaleString() + " - " + escapeHTML(entry.level) + "</span>" +
          "<pre>" + escapeHTML(entry.message) + "</pre>" +
        "</div>"
      ).join("") || "<p>No activity yet.</p>";
    }

    function watchFormEdits() {
      [fields.topic, fields.interval, fields.offset, fields.custom].forEach((input) => {
        input.addEventListener("input", markFormDirty);
      });
      [fields.retain, fields.primary, fields.secondary].forEach((input) => {
        input.addEventListener("change", markFormDirty);
      });
    }

    function markFormDirty() {
      state.formDirty = true;
      updateConnectionTopic(fields.topic.value.trim());
    }

    function hydrateForm(status) {
      syncInput(fields.topic, status.topic || fields.topic.value.trim());
      updateConnectionTopic(fields.topic.value.trim() || status.topic);
      syncInput(fields.interval, status.interval_seconds || fields.interval.value);
      fields.retain.checked = Boolean(status.retain);
      fields.primary.checked = Boolean(status.primary_enabled);
      fields.secondary.checked = Boolean(status.secondary_enabled);
      syncInput(fields.offset, status.timestamp_offset_seconds || 0);
      state.formHydrated = true;
    }

    function syncInput(input, value) {
      input.value = value;
    }

    function updateSettingsLink(url) {
      if (!url) {
        fields.settingsFooter.hidden = true;
        fields.settingsLink.hidden = true;
        fields.settingsLink.removeAttribute("href");
        return;
      }
      fields.settingsLink.href = url;
      fields.settingsFooter.hidden = false;
      fields.settingsLink.hidden = false;
    }

    function row(key, value) {
      return "<div class=\"row\"><span class=\"key\">" + escapeHTML(key) + "</span><span class=\"value\">" + value + "</span></div>";
    }

    function renderConnectionDetails(details) {
      fields.connectionDetails.innerHTML = details.map(([key, value]) =>
        "<div class=\"copy-row\" data-key=\"" + escapeHTML(key) + "\">" +
          "<span class=\"key\">" + escapeHTML(key) + "</span>" +
          "<span class=\"copy-value\">" + escapeHTML(value) + "</span>" +
          "<button class=\"copy-button\" type=\"button\" aria-label=\"Copy " + escapeHTML(key) + "\" data-copy=\"" + escapeHTML(value) + "\">" +
            copyIcon() +
          "</button>" +
        "</div>"
      ).join("");
    }

    function updateConnectionTopic(topic) {
      const normalized = topic || "maestro/target";
      connectionDefaults[4][1] = normalized;
      renderConnectionDetails(connectionDefaults);
    }

    async function copyText(value, button) {
      try {
        if (navigator.clipboard && window.isSecureContext) {
          await navigator.clipboard.writeText(value);
        } else {
          fallbackCopy(value);
        }
        flashCopied(button);
      } catch (error) {
        renderError(error);
      }
    }

    function fallbackCopy(value) {
      const textarea = document.createElement("textarea");
      textarea.value = value;
      textarea.setAttribute("readonly", "");
      textarea.style.position = "fixed";
      textarea.style.left = "-9999px";
      document.body.appendChild(textarea);
      textarea.select();
      document.execCommand("copy");
      textarea.remove();
    }

    function flashCopied(button) {
      const previous = button.innerHTML;
      const previousLabel = button.getAttribute("aria-label");
      if (button.classList.contains("copy-button")) {
        button.innerHTML = checkIcon();
      } else {
        button.textContent = "Copied";
      }
      button.setAttribute("aria-label", "Copied");
      window.setTimeout(() => {
        button.innerHTML = previous;
        if (previousLabel) button.setAttribute("aria-label", previousLabel);
      }, 1200);
    }

    function copyIcon() {
      return "<svg viewBox=\"0 0 24 24\" aria-hidden=\"true\"><rect x=\"9\" y=\"9\" width=\"13\" height=\"13\" rx=\"2\"></rect><path d=\"M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1\"></path></svg>";
    }

    function checkIcon() {
      return "<svg viewBox=\"0 0 24 24\" aria-hidden=\"true\"><path d=\"M20 6 9 17l-5-5\"></path></svg>";
    }

    function pill(target) {
      const cls = target === "OFF" ? "pill off" : "pill";
      return "<span class=\"" + cls + "\">" + escapeHTML(target) + "</span>";
    }

    function formatDate(value) {
      if (!value || String(value).startsWith("0001-01-01T")) return "-";
      return new Date(value).toLocaleString();
    }

    function renderError(error) {
      fields.logs.insertAdjacentHTML("afterbegin",
        "<div class=\"log-entry error\">" +
          "<span class=\"log-time\">" + new Date().toLocaleString() + " - error</span>" +
          "<pre>" + escapeHTML(error.message) + "</pre>" +
        "</div>"
      );
    }

    function setBusy(busy) {
      state.busy = busy;
      document.querySelectorAll("button").forEach((button) => {
        if (button.id !== "refresh") button.disabled = busy;
      });
    }

    function escapeHTML(value) {
      return String(value)
        .replaceAll("&", "&amp;")
        .replaceAll("<", "&lt;")
        .replaceAll(">", "&gt;")
        .replaceAll('"', "&quot;")
        .replaceAll("'", "&#39;");
    }

    refresh();
    setInterval(refresh, 2000);
  </script>
</body>
</html>`
