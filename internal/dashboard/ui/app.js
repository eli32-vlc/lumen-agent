(function () {
  const basePath = window.LUMEN_DASHBOARD_BASE_PATH || "";
  const stateUrl = `${basePath === "/" ? "" : basePath}/api/state`;

  const graphFrame = document.getElementById("graph-frame");
  const graphStage = document.getElementById("graph-stage");
  const graphLines = document.getElementById("graph-lines");
  const graphStats = document.getElementById("graph-stats");
  const toolBranch = document.getElementById("tool-branch");
  const logsContainer = document.getElementById("logs");
  const detailTitle = document.getElementById("detail-title");
  const detailBody = document.getElementById("detail-body");
  const lastUpdated = document.getElementById("last-updated");
  const pollStatus = document.getElementById("poll-status");
  const graphHint = document.getElementById("graph-hint");
  const minimapSvg = document.getElementById("minimap-svg");
  const zoomOutButton = document.getElementById("zoom-out");
  const zoomInButton = document.getElementById("zoom-in");
  const zoomFitButton = document.getElementById("zoom-fit");
  const zoomResetButton = document.getElementById("zoom-reset");
  const toggleActiveOnlyButton = document.getElementById("toggle-active-only");

  const baseNodes = {
    discord: document.getElementById("node-discord"),
    agent: document.getElementById("node-agent"),
    llms: document.getElementById("node-llms"),
    tool: document.getElementById("node-tool"),
  };

  const baseLayout = {
    discord: { x: 72, y: 250 },
    agent: { x: 330, y: 250 },
    llms: { x: 588, y: 250 },
    tool: { x: 846, y: 250 },
  };

  const activePollMS = 900;
  const backgroundPollMS = 2500;
  const nodeWidth = 172;
  const nodeHeight = 78;
  const toolWidth = 182;
  const toolHeight = 88;
  const maxToolRows = 4;
  const toolColumnGap = 92;
  const toolRowGap = 108;

  let latestState = null;
  let latestToolCalls = [];
  let selectedNode = "tool";
  let pollTimer = null;
  let pollInFlight = false;
  let lastSuccessAt = 0;
  let graphScale = 1;
  let hasAutoFit = false;
  let contentWidth = 1320;
  let contentHeight = 620;
  let activeOnly = false;
  let isPanning = false;
  let panStartX = 0;
  let panStartY = 0;
  let panScrollLeft = 0;
  let panScrollTop = 0;

  function positionNode(element, layout, width, height) {
    element.style.left = `${layout.x}px`;
    element.style.top = `${layout.y}px`;
    element.style.width = `${width}px`;
    element.style.minHeight = `${height}px`;
  }

  function pathBetween(from, to) {
    const startX = from.x + from.width;
    const startY = from.y + from.height / 2;
    const endX = to.x;
    const endY = to.y + to.height / 2;
    const midX = startX + (endX - startX) / 2;
    return `M ${startX} ${startY} L ${midX} ${startY} L ${midX} ${endY} L ${endX} ${endY}`;
  }

  function clampScale(value) {
    return Math.max(0.45, Math.min(1.2, value));
  }

  function applyScale() {
    graphStage.style.transform = `scale(${graphScale})`;
    zoomResetButton.textContent = `${Math.round(graphScale * 100)}%`;
    updateMinimap();
  }

  function fitGraphToFrame(nextWidth, nextHeight) {
    const frameWidth = graphFrame.clientWidth - 24;
    const frameHeight = graphFrame.clientHeight - 24;
    if (frameWidth <= 0 || frameHeight <= 0) {
      return;
    }

    const widthScale = frameWidth / nextWidth;
    const heightScale = frameHeight / nextHeight;
    graphScale = clampScale(Math.min(widthScale, heightScale, 1));
    applyScale();
    graphFrame.scrollLeft = 0;
    graphFrame.scrollTop = 0;
  }

  function updateMinimap() {
    if (!contentWidth || !contentHeight) {
      return;
    }
    const miniWidth = 320;
    const miniHeight = 180;
    const viewportWidth = Math.max(12, (graphFrame.clientWidth / (contentWidth * graphScale)) * miniWidth);
    const viewportHeight = Math.max(12, (graphFrame.clientHeight / (contentHeight * graphScale)) * miniHeight);
    const viewportX = ((graphFrame.scrollLeft / graphScale) / contentWidth) * miniWidth;
    const viewportY = ((graphFrame.scrollTop / graphScale) / contentHeight) * miniHeight;
    const scaleX = miniWidth / contentWidth;
    const scaleY = miniHeight / contentHeight;

    const nodeRects = [];
    Object.entries(baseLayout).forEach(([id, layout]) => {
      nodeRects.push(
        `<rect class="minimap-node${latestState && latestState.nodes && latestState.nodes.find((node) => node.id === id && node.active) ? " is-active" : ""}" x="${layout.x * scaleX}" y="${layout.y * scaleY}" width="${nodeWidth * scaleX}" height="${nodeHeight * scaleY}"></rect>`
      );
    });

    Array.from(toolBranch.querySelectorAll(".graph-node")).forEach((element) => {
      const x = parseFloat(element.style.left || "0");
      const y = parseFloat(element.style.top || "0");
      const active = element.classList.contains("is-active");
      if (element.classList.contains("is-hidden")) {
        return;
      }
      nodeRects.push(
        `<rect class="minimap-node${active ? " is-active" : ""}" x="${x * scaleX}" y="${y * scaleY}" width="${toolWidth * scaleX}" height="${toolHeight * scaleY}"></rect>`
      );
    });

    const linePaths = Array.from(graphLines.querySelectorAll("path")).map((path) => {
      const active = path.classList.contains("is-active");
      return `<path class="minimap-line${active ? " is-active" : ""}" d="${path.getAttribute("d")}"></path>`;
    }).join("");

    minimapSvg.innerHTML = `
      <g transform="scale(${scaleX}, ${scaleY})">${linePaths}</g>
      ${nodeRects.join("")}
      <rect class="minimap-viewport" x="${viewportX}" y="${viewportY}" width="${viewportWidth}" height="${viewportHeight}"></rect>
    `;
  }

  function updateGraphStats(toolNodes, state) {
    const activeNodes = (state.nodes || []).filter((node) => node.active).length;
    const failedTools = toolNodes.filter((node) => node.call && node.call.success === false).length;
    const visibleTools = Array.from(toolBranch.querySelectorAll(".graph-node")).filter((node) => !node.classList.contains("is-hidden")).length;
    graphStats.innerHTML = [
      `nodes ${activeNodes}/${(state.nodes || []).length}`,
      `tools ${visibleTools}`,
      `fails ${failedTools}`,
      `calls ${(state.tool_calls || []).length}`,
    ].map((label) => `<div class="stat-chip">${escapeHtml(label)}</div>`).join("");
  }

  function setPollStatus(label, mode) {
    pollStatus.textContent = label;
    pollStatus.classList.toggle("is-error", mode === "error");
    pollStatus.classList.toggle("is-paused", mode === "paused");
  }

  function nextPollDelay() {
    return document.hidden ? backgroundPollMS : activePollMS;
  }

  function queuePoll(delay) {
    window.clearTimeout(pollTimer);
    pollTimer = window.setTimeout(loadState, delay);
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

  function uniqueTools(toolCalls) {
    const seen = new Map();
    toolCalls.forEach((call) => {
      const name = (call.tool || "").trim();
      if (!name || seen.has(name)) {
        return;
      }
      seen.set(name, call);
    });
    return Array.from(seen.entries()).map(([name, call]) => ({
      id: `tool:${name}`,
      name,
      call,
    }));
  }

  function renderLogs(logs) {
    if (!logs.length) {
      logsContainer.innerHTML = '<div class="empty-state">No logs</div>';
      return;
    }

    logsContainer.innerHTML = logs.map((entry) => {
      const summary = entry.session_id ? `session ${entry.session_id}` : "runtime";
      return `
        <article class="log-entry">
          <div class="log-head">
            <div class="detail-title">${escapeHtml(entry.kind)}</div>
            <div class="log-meta">${escapeHtml(formatTime(entry.time))}</div>
          </div>
          <div class="log-meta">${escapeHtml(summary)}</div>
          <pre class="log-json">${escapeHtml(JSON.stringify(entry.data || {}, null, 2))}</pre>
        </article>
      `;
    }).join("");
  }

  function renderDetails(toolNodes) {
    if (!latestState) {
      detailTitle.textContent = "Details";
      detailBody.innerHTML = '<div class="empty-state">Waiting</div>';
      return;
    }

    const nodeMap = Object.fromEntries((latestState.nodes || []).map((node) => [node.id, node]));
    const selectedTool = toolNodes.find((node) => node.id === selectedNode);

    if (selectedTool) {
      const matchingCalls = latestToolCalls.filter((call) => call.tool === selectedTool.name);
      detailTitle.textContent = selectedTool.name;
      if (!matchingCalls.length) {
        detailBody.innerHTML = '<div class="empty-state">No calls</div>';
        return;
      }
      detailBody.innerHTML = matchingCalls.map((call) => {
        const summaryParts = [
          call.kind,
          call.session_id ? `session ${call.session_id}` : "",
          call.duration_ms ? `${call.duration_ms} ms` : "",
          typeof call.success === "boolean" ? (call.success ? "success" : "failed") : "",
        ].filter(Boolean);
        const verbose = call.full_detail || call.detail || JSON.stringify(call.raw || {}, null, 2);
        return `
          <article class="tool-call">
            <div class="tool-call-head">
              <div class="detail-title">${escapeHtml(selectedTool.name)}</div>
              <div class="tool-call-meta">${escapeHtml(formatTime(call.time))}</div>
            </div>
            <div class="tool-call-meta">${escapeHtml(summaryParts.join(" · "))}</div>
            <pre class="tool-call-code">${escapeHtml(verbose || "")}</pre>
          </article>
        `;
      }).join("");
      return;
    }

    if (selectedNode === "tool") {
      detailTitle.textContent = "Tools";
      detailBody.innerHTML = `
        <article class="detail-card">
          <div class="detail-stack">
            <div>
              <div class="detail-key">Recent tools</div>
              <div class="detail-value">${toolNodes.length}</div>
            </div>
            <div>
              <div class="detail-key">Recent calls</div>
              <div class="detail-value">${latestToolCalls.length}</div>
            </div>
          </div>
        </article>
      `;
      return;
    }

    const baseNode = nodeMap[selectedNode];
    if (baseNode) {
      detailTitle.textContent = selectedNode;
      detailBody.innerHTML = `
        <article class="detail-card">
          <div class="detail-stack">
            <div>
              <div class="detail-key">Status</div>
              <div class="detail-value">${baseNode.active ? "active" : "idle"}</div>
            </div>
            <div>
              <div class="detail-key">Updated</div>
              <div class="detail-value">${escapeHtml(formatTime(latestState.generated_at))}</div>
            </div>
          </div>
        </article>
      `;
      return;
    }

    detailTitle.textContent = "Details";
    detailBody.innerHTML = '<div class="empty-state">Select a node</div>';
  }

  function renderGraph(state) {
    const nodeStates = Object.fromEntries((state.nodes || []).map((node) => [node.id, node.active]));
    const toolNodes = uniqueTools(latestToolCalls);

    Object.entries(baseNodes).forEach(([id, element]) => {
      const layout = baseLayout[id];
      positionNode(element, layout, nodeWidth, nodeHeight);
      element.classList.toggle("is-active", Boolean(nodeStates[id]));
      element.classList.toggle("is-selected", selectedNode === id);
    });

    const branchStartX = 1110;
    const branchCenterY = 250;
    const toolColumns = Math.max(1, Math.ceil(toolNodes.length / maxToolRows));

    function toolLayout(index) {
      const column = Math.floor(index / maxToolRows);
      const row = index % maxToolRows;
      const rowsInColumn = Math.min(maxToolRows, toolNodes.length - column * maxToolRows);
      const columnCenterOffset = ((rowsInColumn - 1) * toolRowGap) / 2;
      return {
        x: branchStartX + column * (toolWidth + toolColumnGap),
        y: branchCenterY - columnCenterOffset + row * toolRowGap,
      };
    }

    toolBranch.innerHTML = toolNodes.map((node, index) => {
      const layout = toolLayout(index);
      return `
        <button
          class="graph-node is-tool ${node.call && node.call.success === false ? "is-failed" : ""}"
          type="button"
          data-node-id="${escapeHtml(node.id)}"
          style="left:${layout.x}px; top:${layout.y}px;"
        >
          <span class="node-label">${escapeHtml(node.name)}</span>
          <span class="node-meta">${escapeHtml(formatTime(node.call.time))}</span>
        </button>
      `;
    }).join("");

    Array.from(toolBranch.querySelectorAll(".graph-node")).forEach((element) => {
      const id = element.dataset.nodeId;
      const node = toolNodes.find((item) => item.id === id);
      if (!node) {
        return;
      }
      const active = node.call && typeof node.call.success === "boolean" ? node.call.success : true;
      const hidden = activeOnly && !active;
      element.classList.toggle("is-active", Boolean(active));
      element.classList.toggle("is-selected", selectedNode === id);
      element.classList.toggle("is-hidden", hidden);
      element.addEventListener("click", () => {
        selectedNode = id;
        renderGraph(latestState);
        renderDetails(toolNodes);
      });
    });

    const lines = [];
    const baseBoxes = {
      discord: { x: baseLayout.discord.x, y: baseLayout.discord.y, width: nodeWidth, height: nodeHeight },
      agent: { x: baseLayout.agent.x, y: baseLayout.agent.y, width: nodeWidth, height: nodeHeight },
      llms: { x: baseLayout.llms.x, y: baseLayout.llms.y, width: nodeWidth, height: nodeHeight },
      tool: { x: baseLayout.tool.x, y: baseLayout.tool.y, width: nodeWidth, height: nodeHeight },
    };

    [
      ["discord", "agent"],
      ["agent", "llms"],
      ["llms", "tool"],
    ].forEach(([from, to]) => {
      lines.push({
        path: pathBetween(baseBoxes[from], baseBoxes[to]),
        active: Boolean(nodeStates[from] && nodeStates[to]),
      });
    });

    toolNodes.forEach((node, index) => {
      const layout = toolLayout(index);
      const active = node.call ? node.call.success !== false : false;
      if (activeOnly && !active) {
        return;
      }
      const toolBox = { x: layout.x, y: layout.y, width: toolWidth, height: toolHeight };
      lines.push({
        path: pathBetween(baseBoxes.tool, toolBox),
        active,
      });
    });

    contentWidth = Math.max(1320, branchStartX + toolColumns * (toolWidth + toolColumnGap) + 120);
    contentHeight = Math.max(620, branchCenterY + Math.ceil(Math.min(toolNodes.length, maxToolRows) / 2) * toolRowGap + 180);
    graphStage.style.minWidth = `${contentWidth}px`;
    graphStage.style.minHeight = `${contentHeight}px`;
    graphLines.setAttribute("viewBox", `0 0 ${contentWidth} ${contentHeight}`);
    graphLines.innerHTML = lines.map((line) => (
      `<path class="graph-line${line.active ? " is-active" : ""}" d="${line.path}"></path>`
    )).join("");

    if (!hasAutoFit) {
      fitGraphToFrame(contentWidth, contentHeight);
      hasAutoFit = true;
    } else {
      updateMinimap();
    }
    updateGraphStats(toolNodes, state);
    renderDetails(toolNodes);
  }

  function applyState(state) {
    latestState = state;
    latestToolCalls = state.tool_calls || [];
    renderGraph(state);
    renderLogs(state.logs || []);
    lastUpdated.textContent = state.generated_at ? formatTime(state.generated_at) : "Waiting";
    setPollStatus(document.hidden ? "Background" : "Live", "");
  }

  async function loadState() {
    if (pollInFlight) {
      return;
    }

    pollInFlight = true;
    try {
      const response = await fetch(`${stateUrl}?limit=160&tool_limit=80`, { cache: "no-store" });
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const state = await response.json();
      lastSuccessAt = Date.now();
      applyState(state);
    } catch (error) {
      const staleForMS = lastSuccessAt ? Date.now() - lastSuccessAt : 0;
      setPollStatus("Error", "error");
      lastUpdated.textContent = staleForMS > 0 ? `${Math.round(staleForMS / 1000)}s stale` : "Unavailable";
    } finally {
      pollInFlight = false;
      queuePoll(nextPollDelay());
    }
  }

  Object.entries(baseNodes).forEach(([id, element]) => {
    element.addEventListener("click", () => {
      selectedNode = id;
      if (latestState) {
        renderGraph(latestState);
      }
    });
  });

  document.addEventListener("visibilitychange", () => {
    if (document.hidden) {
      setPollStatus("Background", "paused");
    }
    queuePoll(80);
  });

  graphFrame.addEventListener("pointerdown", (event) => {
    if (event.target.closest(".graph-node")) {
      return;
    }
    isPanning = true;
    panStartX = event.clientX;
    panStartY = event.clientY;
    panScrollLeft = graphFrame.scrollLeft;
    panScrollTop = graphFrame.scrollTop;
    graphFrame.setPointerCapture(event.pointerId);
    graphHint.textContent = "Panning";
  });

  graphFrame.addEventListener("pointermove", (event) => {
    if (!isPanning) {
      return;
    }
    graphFrame.scrollLeft = panScrollLeft - (event.clientX - panStartX);
    graphFrame.scrollTop = panScrollTop - (event.clientY - panStartY);
    updateMinimap();
  });

  graphFrame.addEventListener("pointerup", (event) => {
    if (!isPanning) {
      return;
    }
    isPanning = false;
    graphHint.textContent = "Drag background to pan";
    graphFrame.releasePointerCapture(event.pointerId);
  });

  graphFrame.addEventListener("pointercancel", () => {
    isPanning = false;
    graphHint.textContent = "Drag background to pan";
  });

  graphFrame.addEventListener("wheel", (event) => {
    if (!event.ctrlKey && !event.metaKey) {
      return;
    }
    event.preventDefault();
    graphScale = clampScale(graphScale + (event.deltaY < 0 ? 0.08 : -0.08));
    applyScale();
  }, { passive: false });

  graphFrame.addEventListener("scroll", () => {
    updateMinimap();
  });

  zoomOutButton.addEventListener("click", () => {
    graphScale = clampScale(graphScale - 0.08);
    applyScale();
  });

  zoomInButton.addEventListener("click", () => {
    graphScale = clampScale(graphScale + 0.08);
    applyScale();
  });

  zoomFitButton.addEventListener("click", () => {
    fitGraphToFrame(contentWidth, contentHeight);
  });

  zoomResetButton.addEventListener("click", () => {
    graphScale = 1;
    applyScale();
  });

  toggleActiveOnlyButton.addEventListener("click", () => {
    activeOnly = !activeOnly;
    toggleActiveOnlyButton.textContent = activeOnly ? "Active" : "All";
    toggleActiveOnlyButton.classList.toggle("is-active", activeOnly);
    if (latestState) {
      renderGraph(latestState);
    }
  });

  window.addEventListener("resize", () => {
    updateMinimap();
  });

  loadState();
})();
