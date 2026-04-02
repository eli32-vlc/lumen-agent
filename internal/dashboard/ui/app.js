(function () {
  const basePath = window.LUMEN_DASHBOARD_BASE_PATH || "";
  const stateUrl = `${basePath === "/" ? "" : basePath}/api/state`;
  const tabButtons = Array.from(document.querySelectorAll(".tab-button"));
  const panels = Array.from(document.querySelectorAll(".panel"));
  const graphStage = document.getElementById("graph-stage");
  const graphFrame = document.getElementById("graph-frame");
  const toolPanel = document.getElementById("tool-panel");
  const toolCallsContainer = document.getElementById("tool-calls");
  const logsContainer = document.getElementById("logs");
  const lastUpdated = document.getElementById("last-updated");
  const zoomReset = document.getElementById("zoom-reset");
  const nodes = {
    discord: document.getElementById("node-discord"),
    agent: document.getElementById("node-agent"),
    llms: document.getElementById("node-llms"),
    tool: document.getElementById("node-tool"),
  };
  const edges = {
    "discord-agent": document.getElementById("edge-discord-agent"),
    "agent-llms": document.getElementById("edge-agent-llms"),
    "llms-tool": document.getElementById("edge-llms-tool"),
  };

  let scale = 1;
  let selectedNode = "";
  let latestToolCalls = [];

  function setScale(nextScale) {
    scale = Math.max(0.6, Math.min(1.8, nextScale));
    graphStage.style.transform = `scale(${scale})`;
    zoomReset.textContent = `${Math.round(scale * 100)}%`;
  }

  function switchTab(name) {
    tabButtons.forEach((button) => {
      button.classList.toggle("is-active", button.dataset.tab === name);
    });
    panels.forEach((panel) => {
      panel.classList.toggle("is-active", panel.dataset.panel === name);
    });
  }

  function renderToolCalls(toolCalls) {
    if (selectedNode !== "tool") {
      toolPanel.hidden = true;
      toolCallsContainer.innerHTML = "";
      return;
    }

    toolPanel.hidden = false;
    if (!toolCalls.length) {
      toolCallsContainer.innerHTML = '<div class="empty-state">No tool calls yet.</div>';
      return;
    }

    toolCallsContainer.innerHTML = toolCalls.map((call) => {
      const summaryParts = [
        call.kind,
        call.session_id ? `session ${call.session_id}` : "",
        call.duration_ms ? `${call.duration_ms} ms` : "",
        typeof call.success === "boolean" ? (call.success ? "success" : "failed") : "",
      ].filter(Boolean);

      const verbose = call.full_detail || call.detail || JSON.stringify(call.raw || {}, null, 2);
      return `
        <article class="tool-call">
          <div class="tool-call-header">
            <div class="tool-call-title">${escapeHtml(call.tool || "tool")}</div>
            <div class="tool-call-meta">${escapeHtml(formatTime(call.time))}</div>
          </div>
          <div class="tool-call-meta">${escapeHtml(summaryParts.join(" · "))}</div>
          <pre class="tool-call-code">${escapeHtml(verbose || "")}</pre>
        </article>
      `;
    }).join("");
  }

  function renderLogs(logs) {
    if (!logs.length) {
      logsContainer.innerHTML = '<div class="empty-state">No logs yet.</div>';
      return;
    }

    logsContainer.innerHTML = logs.map((entry) => {
      const summary = entry.session_id ? `session ${entry.session_id}` : "runtime";
      return `
        <article class="log-entry">
          <div class="log-header">
            <div class="log-title">${escapeHtml(entry.kind)}</div>
            <div class="log-meta">${escapeHtml(formatTime(entry.time))}</div>
          </div>
          <div class="log-meta">${escapeHtml(summary)}</div>
          <pre class="log-json">${escapeHtml(JSON.stringify(entry.data || {}, null, 2))}</pre>
        </article>
      `;
    }).join("");
  }

  function applyState(state) {
    const nodeStates = Object.fromEntries((state.nodes || []).map((node) => [node.id, node.active]));
    const edgeStates = Object.fromEntries((state.edges || []).map((edge) => [edge.id, edge.active]));

    latestToolCalls = state.tool_calls || [];

    Object.entries(nodes).forEach(([id, element]) => {
      element.classList.toggle("is-active", Boolean(nodeStates[id]));
      element.classList.toggle("is-selected", selectedNode === id);
    });

    Object.entries(edges).forEach(([id, element]) => {
      element.classList.toggle("is-active", Boolean(edgeStates[id]));
    });

    renderToolCalls(latestToolCalls);
    renderLogs(state.logs || []);
    lastUpdated.textContent = state.generated_at ? `Updated ${formatTime(state.generated_at)}` : "Waiting for logs";
  }

  async function loadState() {
    try {
      const response = await fetch(`${stateUrl}?limit=150&tool_limit=80`, { cache: "no-store" });
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const state = await response.json();
      applyState(state);
    } catch (error) {
      lastUpdated.textContent = "Dashboard unavailable";
    }
  }

  function formatTime(value) {
    if (!value) {
      return "";
    }
    const parsed = new Date(value);
    if (Number.isNaN(parsed.getTime())) {
      return value;
    }
    return parsed.toLocaleString();
  }

  function escapeHtml(value) {
    return String(value)
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;");
  }

  tabButtons.forEach((button) => {
    button.addEventListener("click", () => switchTab(button.dataset.tab));
  });

  Object.entries(nodes).forEach(([id, element]) => {
    element.addEventListener("click", () => {
      if (id !== "tool") {
        selectedNode = "";
        Object.entries(nodes).forEach(([, nodeElement]) => {
          nodeElement.classList.remove("is-selected");
        });
        renderToolCalls(latestToolCalls);
        return;
      }

      selectedNode = selectedNode === id ? "" : id;
      Object.entries(nodes).forEach(([nodeID, nodeElement]) => {
        nodeElement.classList.toggle("is-selected", selectedNode === nodeID);
      });
      renderToolCalls(latestToolCalls);
    });
  });

  document.getElementById("zoom-in").addEventListener("click", () => setScale(scale + 0.1));
  document.getElementById("zoom-out").addEventListener("click", () => setScale(scale - 0.1));
  zoomReset.addEventListener("click", () => setScale(1));
  graphFrame.addEventListener("wheel", (event) => {
    if (!event.ctrlKey && !event.metaKey) {
      return;
    }
    event.preventDefault();
    setScale(scale + (event.deltaY < 0 ? 0.08 : -0.08));
  }, { passive: false });

  setScale(1);
  loadState();
  window.setInterval(loadState, 1500);
})();
