(function () {
  const basePath = window.ELEMENT_ORION_DASHBOARD_BASE_PATH || "";
  const stateUrl = `${basePath === "/" ? "" : basePath}/api/state`;

  const metricsGrid = document.getElementById("metrics-grid");
  const tableSummary = document.getElementById("table-summary");
  const tableMeta = document.getElementById("table-meta");
  const tableBody = document.getElementById("events-table-body");
  const lastUpdated = document.getElementById("last-updated");
  const pollStatus = document.getElementById("poll-status");
  const filterType = document.getElementById("filter-type");
  const filterScope = document.getElementById("filter-scope");
  const filterStatus = document.getElementById("filter-status");
  const filterSearch = document.getElementById("filter-search");
  const sortButtons = Array.from(document.querySelectorAll(".sort-button"));

  const activePollMS = 1000;
  const backgroundPollMS = 2500;

  let latestState = null;
  let latestEvents = [];
  let pollTimer = null;
  let pollInFlight = false;
  let lastSuccessAt = 0;
  let sortState = { key: "time", direction: "desc" };
  let expandedEventKey = "";

  function escapeHtml(value) {
    return String(value)
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;")
      .replaceAll("'", "&#39;");
  }

  function formatTime(value) {
    if (!value) {
      return "—";
    }
    const parsed = new Date(value);
    if (Number.isNaN(parsed.getTime())) {
      return value;
    }
    return parsed.toLocaleString();
  }

  function formatCompactTime(value) {
    if (!value) {
      return "—";
    }
    const parsed = new Date(value);
    if (Number.isNaN(parsed.getTime())) {
      return value;
    }
    return parsed.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
  }

  function formatNumber(value) {
    return new Intl.NumberFormat().format(Number(value) || 0);
  }

  function formatPercent(value) {
    if (!Number.isFinite(value)) {
      return "—";
    }
    return `${value.toFixed(1)}%`;
  }

  function formatDuration(value) {
    if (!value) {
      return "—";
    }
    if (value < 1000) {
      return `${Math.round(value)} ms`;
    }
    return `${(value / 1000).toFixed(2)} s`;
  }

  function formatJson(value) {
    if (value === null || value === undefined || value === "") {
      return "—";
    }
    if (typeof value === "string") {
      return value;
    }
    try {
      return JSON.stringify(value, null, 2);
    } catch (_error) {
      return String(value);
    }
  }

  function formatBytes(value) {
    const size = Number(value) || 0;
    if (size < 1024) {
      return `${size} B`;
    }
    if (size < 1024 * 1024) {
      return `${(size / 1024).toFixed(1)} KB`;
    }
    return `${(size / (1024 * 1024)).toFixed(2)} MB`;
  }

  function toNumber(value) {
    if (typeof value === "number" && Number.isFinite(value)) {
      return value;
    }
    if (typeof value === "string" && value.trim() !== "") {
      const parsed = Number(value);
      if (Number.isFinite(parsed)) {
        return parsed;
      }
    }
    return 0;
  }

  function nextPollDelay() {
    return document.hidden ? backgroundPollMS : activePollMS;
  }

  function queuePoll(delay) {
    window.clearTimeout(pollTimer);
    pollTimer = window.setTimeout(loadState, delay);
  }

  function setPollStatus(label, mode) {
    pollStatus.textContent = label;
    pollStatus.classList.toggle("is-error", mode === "error");
    pollStatus.classList.toggle("is-paused", mode === "paused");
  }

  function eventCategory(kind) {
    if (kind.includes("tool")) {
      return "tool";
    }
    if (kind.includes("model")) {
      return "model";
    }
    if (kind.includes("status")) {
      return "status";
    }
    if (kind.includes("assistant")) {
      return "reply";
    }
    if (kind === "turn_start") {
      return "turn";
    }
    return "event";
  }

  function eventOrigin(kind) {
    return kind.startsWith("background_") ? "background" : "foreground";
  }

  function eventStatus(kind, data) {
    if (kind === "tool_start" || kind === "background_tool_start") {
      return "running";
    }
    if (kind === "tool_done" || kind === "background_tool_done") {
      if (data && Object.prototype.hasOwnProperty.call(data, "success")) {
        return data.success ? "ok" : "failed";
      }
      return "info";
    }
    if (kind === "turn_start") {
      return "open";
    }
    if (kind === "assistant_reply" || kind === "background_assistant") {
      return "sent";
    }
    if (kind === "model_done" || kind === "background_model_done") {
      return "ok";
    }
    return "info";
  }

  function eventDetail(kind, data) {
    if (!data) {
      return "—";
    }
    if (kind.includes("tool")) {
      const tool = String(data.tool || "tool");
      const detail = String(data.detail || "").trim();
      return detail ? `${tool} · ${detail}` : tool;
    }
    if (kind.includes("model")) {
      return "completion";
    }
    if (kind.includes("status")) {
      return String(data.message || data.detail || "status");
    }
    if (kind === "turn_start") {
      return String(data.kind || "user");
    }
    if (kind === "assistant_reply" || kind === "background_assistant") {
      const length = toNumber(data.length);
      return length > 0 ? `reply length ${length}` : "assistant reply";
    }
    return JSON.stringify(data);
  }

  function normalizeEvent(entry) {
    const data = entry.data || {};
    const kind = String(entry.kind || "event");
    const timestamp = Date.parse(entry.time || "");
    const detail = eventDetail(kind, data);
    const tokens = toNumber(data.tokens);
    const duration = toNumber(data.duration_ms);
    const status = eventStatus(kind, data);
    const origin = eventOrigin(kind);
    const category = eventCategory(kind);
    const session = String(entry.session_id || "runtime");

    return {
      key: [entry.time || "", kind, session, JSON.stringify(data)].join("::"),
      time: entry.time || "",
      timeLabel: formatTime(entry.time),
      timestamp: Number.isNaN(timestamp) ? 0 : timestamp,
      session,
      kind,
      detail,
      tokens,
      duration,
      status,
      category,
      origin,
      rawData: data,
      searchText: [
        session,
        kind,
        detail,
        status,
        origin,
        category,
        JSON.stringify(data),
      ].join(" ").toLowerCase(),
    };
  }

  function compareValues(left, right, direction) {
    if (left === right) {
      return 0;
    }
    if (typeof left === "number" && typeof right === "number") {
      return direction === "asc" ? left - right : right - left;
    }
    return direction === "asc"
      ? String(left).localeCompare(String(right))
      : String(right).localeCompare(String(left));
  }

  function filteredAndSortedEvents() {
    const typeValue = filterType.value;
    const scopeValue = filterScope.value;
    const statusValue = filterStatus.value;
    const searchValue = filterSearch.value.trim().toLowerCase();

    const filtered = latestEvents.filter((event) => {
      if (typeValue !== "all" && event.category !== typeValue) {
        return false;
      }
      if (scopeValue !== "all" && event.origin !== scopeValue) {
        return false;
      }
      if (statusValue !== "all" && event.status !== statusValue) {
        return false;
      }
      if (searchValue && !event.searchText.includes(searchValue)) {
        return false;
      }
      return true;
    });

    return filtered.sort((left, right) => {
      switch (sortState.key) {
        case "session":
          return compareValues(left.session, right.session, sortState.direction);
        case "kind":
          return compareValues(left.kind, right.kind, sortState.direction);
        case "detail":
          return compareValues(left.detail, right.detail, sortState.direction);
        case "tokens":
          return compareValues(left.tokens, right.tokens, sortState.direction);
        case "duration":
          return compareValues(left.duration, right.duration, sortState.direction);
        case "status":
          return compareValues(left.status, right.status, sortState.direction);
        case "time":
        default:
          return compareValues(left.timestamp, right.timestamp, sortState.direction);
      }
    });
  }

  function summarizeTrend(values) {
    if (!values.length) {
      return "no recent change";
    }
    const midpoint = Math.ceil(values.length / 2);
    const current = values.slice(0, midpoint).reduce((sum, value) => sum + value, 0);
    const previous = values.slice(midpoint).reduce((sum, value) => sum + value, 0);
    if (!previous) {
      return current ? "new activity in current window" : "no recent change";
    }
    const diff = ((current - previous) / previous) * 100;
    if (Math.abs(diff) < 2) {
      return "steady vs prior window";
    }
    const rounded = Math.round(diff);
    return `${rounded > 0 ? "+" : ""}${rounded}% vs prior window`;
  }

  function renderMetrics(state) {
    const summary = state.summary || {};
    const memory = state.memory || {};
    const modelEvents = latestEvents.filter((event) => event.category === "model").map((event) => event.tokens);
    const toolEvents = latestEvents.filter((event) => event.category === "tool" && event.status !== "running");
    const toolTotals = toolEvents.length;
    const successfulTools = Math.max(0, (summary.recent_tool_calls || toolTotals) - (summary.tool_failures || 0));
    const successRate = toolTotals || summary.recent_tool_calls
      ? (successfulTools / Math.max(1, summary.recent_tool_calls || toolTotals)) * 100
      : NaN;

    const cards = [
      {
        label: "Total Tokens",
        value: formatNumber(summary.total_tokens || 0),
        meta: `${formatNumber(summary.model_calls || 0)} model calls in loaded logs`,
        trend: "cumulative across dashboard log data",
      },
      {
        label: "Tool Success",
        value: formatPercent(successRate),
        meta: `${formatNumber(summary.tool_failures || 0)} failed of ${formatNumber(summary.recent_tool_calls || 0)}`,
        trend: summarizeTrend(toolEvents.map((event) => (event.status === "failed" ? 0 : 1))),
      },
      {
        label: "Active Sessions",
        value: formatNumber(summary.active_sessions || 0),
        meta: `${formatNumber(summary.active_nodes || 0)} nodes active`,
        trend: `${formatNumber(summary.background_events || 0)} background events`,
      },
      {
        label: "Memory Footprint",
        value: memory.available ? formatBytes(memory.total_bytes || 0) : "Unavailable",
        meta: memory.available
          ? `${formatNumber(memory.loaded_shards || 0)} of ${formatNumber(memory.shard_count || 0)} shards loaded`
          : "memory directory not readable",
        trend: memory.compaction_enabled ? "compaction enabled" : "compaction disabled",
      },
    ];

    metricsGrid.innerHTML = cards.map((card) => `
      <article class="metric-card">
        <div class="metric-label">${escapeHtml(card.label)}</div>
        <div class="metric-value">${escapeHtml(card.value)}</div>
        <div class="metric-meta">${escapeHtml(card.meta)}</div>
        <div class="metric-label">${escapeHtml(card.trend)}</div>
      </article>
    `).join("");
  }

  function renderTable() {
    const rows = filteredAndSortedEvents();
    updateSortButtons();

    if (expandedEventKey && !rows.some((event) => event.key === expandedEventKey)) {
      expandedEventKey = "";
    }

    tableSummary.textContent = latestEvents.length
      ? `${rows.length} of ${latestEvents.length} rows visible`
      : "Waiting for runtime data";
    tableMeta.textContent = `Sort: ${sortState.key} ${sortState.direction}`;

    if (!rows.length) {
      tableBody.innerHTML = '<tr><td colspan="7" class="empty-state">No matching events</td></tr>';
      return;
    }

    tableBody.innerHTML = rows.map((event) => {
      const isExpanded = event.key === expandedEventKey;
      const detailSections = [
        ["Kind", event.kind],
        ["Origin", event.origin],
        ["Status", event.status],
        ["Data", formatJson(event.rawData)],
      ];

      return `
        <tr class="event-row ${isExpanded ? "is-expanded" : ""}" data-event-key="${escapeHtml(event.key)}" aria-expanded="${isExpanded ? "true" : "false"}">
          <td title="${escapeHtml(event.timeLabel)}">${escapeHtml(event.timeLabel)}</td>
          <td title="${escapeHtml(event.session)}">${escapeHtml(event.session)}</td>
          <td title="${escapeHtml(event.kind)}">${escapeHtml(event.kind)}</td>
          <td title="${escapeHtml(event.detail)}">${escapeHtml(event.detail)}</td>
          <td>${event.tokens ? escapeHtml(formatNumber(event.tokens)) : "—"}</td>
          <td>${escapeHtml(formatDuration(event.duration))}</td>
          <td class="cell-status status-${escapeHtml(event.status)}">${escapeHtml(event.status)}</td>
        </tr>
        ${isExpanded ? `
          <tr class="event-detail-row">
            <td colspan="7">
              <div class="event-detail">
                ${detailSections.map(([label, value]) => `
                  <div class="event-detail-block">
                    <div class="event-detail-label">${escapeHtml(label)}</div>
                    <div class="event-detail-value">${escapeHtml(String(value))}</div>
                  </div>
                `).join("")}
              </div>
            </td>
          </tr>
        ` : ""}
      `;
    }).join("");
  }

  function updateSortButtons() {
    sortButtons.forEach((button) => {
      const active = button.dataset.sort === sortState.key;
      button.classList.toggle("is-active", active);
      button.textContent = active
        ? `${titleCase(button.dataset.sort)} ${sortState.direction === "asc" ? "↑" : "↓"}`
        : titleCase(button.dataset.sort);
    });
  }

  function titleCase(value) {
    return String(value || "")
      .split("_")
      .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
      .join(" ");
  }

  function render(state) {
    latestState = state;
    latestEvents = (state.logs || []).map(normalizeEvent);
    renderMetrics(state);
    renderTable();
    lastUpdated.textContent = state.generated_at ? formatTime(state.generated_at) : "Waiting";
    setPollStatus(document.hidden ? "Background" : "Live", document.hidden ? "paused" : "");
  }

  async function loadState() {
    if (pollInFlight) {
      return;
    }

    pollInFlight = true;
    try {
      const response = await fetch(`${stateUrl}?limit=200&tool_limit=120`, { cache: "no-store" });
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const state = await response.json();
      lastSuccessAt = Date.now();
      render(state);
    } catch (error) {
      const staleForMS = lastSuccessAt ? Date.now() - lastSuccessAt : 0;
      setPollStatus("Error", "error");
      lastUpdated.textContent = staleForMS > 0 ? `${Math.round(staleForMS / 1000)}s stale` : "Unavailable";
    } finally {
      pollInFlight = false;
      queuePoll(nextPollDelay());
    }
  }

  [filterType, filterScope, filterStatus].forEach((element) => {
    element.addEventListener("change", () => {
      renderTable();
    });
  });

  filterSearch.addEventListener("input", () => {
    renderTable();
  });

  tableBody.addEventListener("click", (event) => {
    const row = event.target.closest(".event-row");
    if (!row) {
      return;
    }
    const key = row.dataset.eventKey || "";
    expandedEventKey = expandedEventKey === key ? "" : key;
    renderTable();
  });

  sortButtons.forEach((button) => {
    button.addEventListener("click", () => {
      const key = button.dataset.sort;
      if (sortState.key === key) {
        sortState.direction = sortState.direction === "asc" ? "desc" : "asc";
      } else {
        sortState.key = key;
        sortState.direction = key === "time" ? "desc" : "asc";
      }
      renderTable();
    });
  });

  document.addEventListener("visibilitychange", () => {
    if (document.hidden) {
      setPollStatus("Background", "paused");
    }
    queuePoll(80);
  });

  loadState();
})();
