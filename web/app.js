const logList = document.getElementById("log-list");
const details = document.getElementById("details");
const refreshButton = document.getElementById("refresh");
const autoRefresh = document.getElementById("auto-refresh");
const searchInput = document.getElementById("search");
const methodFilter = document.getElementById("method-filter");
const statusFilter = document.getElementById("status-filter");
const startTimeInput = document.getElementById("start-time");
const endTimeInput = document.getElementById("end-time");

let logs = [];
let selectedId = null;
let refreshTimer = null;
let lastLogsJson = "";
let expandedSections = new Set();

const fetchLogs = async () => {
  try {
    const response = await fetch("/api/logs");
    if (!response.ok) {
      logList.innerHTML = "<p class='error'>Failed to fetch logs.</p>";
      return;
    }
    const data = await response.json();
    const dataJson = JSON.stringify(data);
    
    if (dataJson === lastLogsJson) {
      return; // No changes, skip re-render
    }
    
    lastLogsJson = dataJson;
    logs = data;
    
    updateFilters(logs);
    renderList();
    
    if (selectedId) {
      const match = logs.find((entry) => entry.id === selectedId);
      if (match) {
        renderDetails(match);
      }
    }
  } catch (err) {
    console.error("Fetch error:", err);
  }
};

const renderList = () => {
  const filtered = applyFilters(logs);
  
  const currentListState = JSON.stringify(filtered.map(e => ({id: e.id, status: e.status, method: e.method, url: e.url})));
  if (logList.dataset.state === currentListState && logList.dataset.selected === String(selectedId)) {
    return;
  }
  logList.dataset.state = currentListState;
  logList.dataset.selected = String(selectedId);

  if (!filtered.length) {
    logList.innerHTML = "<p class='placeholder'>No traffic yet.</p>";
    return;
  }

  logList.innerHTML = "";
  filtered.forEach((entry) => {
    const item = document.createElement("button");
    item.className = "log-entry";
    if (entry.id === selectedId) {
      item.classList.add("log-entry--active");
    }
    item.innerHTML = `
      <div class="log-entry__meta">
        <span class="method">${entry.method}</span>
        <span class="status">${entry.status || "-"}</span>
      </div>
      <div class="log-entry__url">${entry.url}</div>
      <div class="log-entry__time">${new Date(entry.startedAt).toLocaleTimeString()}</div>
    `;
    item.addEventListener("click", () => {
      if (selectedId === entry.id) return;
      selectedId = entry.id;
      renderDetails(entry);
      renderList();
    });
    logList.appendChild(item);
  });
};

const isJson = (str) => {
  if (!str) return false;
  try {
    const obj = JSON.parse(str);
    return !!obj && typeof obj === 'object';
  } catch (e) {
    return false;
  }
};

const renderDetails = (entry) => {
  const detailsState = JSON.stringify({id: entry.id, status: entry.status, duration: entry.durationMillis, error: entry.error});
  if (details.dataset.state === detailsState && details.dataset.selected === String(entry.id)) {
     // Skip if identical
  }
  details.dataset.state = detailsState;
  details.dataset.selected = String(entry.id);

  details.innerHTML = `
    <div class="detail-header">
      <h2>${entry.method} ${entry.url}</h2>
      <p>Target: <span>${entry.target || "Not resolved"}</span></p>
      <p>Status: <strong>${entry.status || "Pending"}</strong> Duration: ${entry.durationMillis} ms</p>
    </div>
    <div class="detail-grid">
      <div class="detail-section">
        <h3>Request</h3>
        <div class="detail-section__scrollable">
          <p><strong>Client:</strong> ${entry.clientIp || ""}</p>
          <p><strong>Content-Type:</strong> ${entry.requestContentType || ""}</p>
          <p><strong>Content-Length:</strong> ${entry.requestContentLength || 0}</p>
          <div class="action-bar">
            ${renderHeaderToggle("request-headers")}
            ${isJson(entry.requestBody) ? `<button class="pretty-print-btn" data-target="request-body" data-type="request">Pretty print</button>` : ""}
          </div>
          <div id="request-headers" class="header-table ${expandedSections.has("request-headers") ? "" : "is-collapsed"}">
            ${renderHeaderTable(entry.requestHeaders)}
          </div>
          ${renderBody(entry.requestBody, entry.requestBodyEncoding, entry.requestBodyTruncated, "request-body")}
        </div>
      </div>
      <div class="detail-section">
        <h3>Response</h3>
        <div class="detail-section__scrollable">
          <p><strong>Content-Type:</strong> ${entry.responseContentType || ""}</p>
          <p><strong>Content-Length:</strong> ${entry.responseContentLength || 0}</p>
          <div class="action-bar">
            ${renderHeaderToggle("response-headers")}
            ${isJson(entry.responseBody) ? `<button class="pretty-print-btn" data-target="response-body" data-type="response">Pretty print</button>` : ""}
          </div>
          <div id="response-headers" class="header-table ${expandedSections.has("response-headers") ? "" : "is-collapsed"}">
            ${renderHeaderTable(entry.responseHeaders)}
          </div>
          ${renderBody(entry.responseBody, entry.responseBodyEncoding, entry.responseBodyTruncated, "response-body")}
        </div>
      </div>
    </div>
    ${entry.error ? `<div class="error-box">Error: ${entry.error}</div>` : ""}
  `;

  details.querySelectorAll(".header-toggle").forEach((button) => {
    button.addEventListener("click", () => {
      const targetId = button.dataset.target;
      const target = document.getElementById(targetId);
      if (!target) return;
      
      const isCurrentlyCollapsed = target.classList.contains("is-collapsed");
      if (isCurrentlyCollapsed) {
        target.classList.remove("is-collapsed");
        button.textContent = "Hide headers";
        button.setAttribute("aria-expanded", "true");
        expandedSections.add(targetId);
      } else {
        target.classList.add("is-collapsed");
        button.textContent = "Show headers";
        button.setAttribute("aria-expanded", "false");
        expandedSections.delete(targetId);
      }
    });
  });

  details.querySelectorAll(".pretty-print-btn").forEach((button) => {
    button.addEventListener("click", () => {
      const type = button.dataset.type;
      const raw = type === 'request' ? entry.requestBody : entry.responseBody;
      const textarea = document.getElementById(button.dataset.target);
      if (textarea) {
        if (button.textContent === "Pretty print") {
          try {
            const obj = JSON.parse(raw);
            textarea.value = JSON.stringify(obj, null, 2);
            button.textContent = "Show raw";
          } catch (e) {
            console.error("Failed to pretty print", e);
          }
        } else {
          textarea.value = raw;
          button.textContent = "Pretty print";
        }
      }
    });
  });
};

const renderHeaderToggle = (sectionId) => {
  const isExpanded = expandedSections.has(sectionId);
  return `
    <button class="header-toggle" type="button" data-target="${sectionId}" aria-expanded="${isExpanded}">
      ${isExpanded ? "Hide headers" : "Show headers"}
    </button>
  `;
};

const renderHeaderTable = (headers) => {
  if (!headers || Object.keys(headers).length === 0) {
    return "<p class='placeholder'>No headers captured.</p>";
  }
  const rows = Object.entries(headers)
    .map(([key, value]) => `<tr><th>${key}</th><td>${value}</td></tr>`)
    .join("");
  return `<table class='headers'>${rows}</table>`;
};

const renderBody = (body, encoding, truncated, id) => {
  if (!body) {
    return "<p class='placeholder'>No body captured.</p>";
  }
  const note = truncated ? "<span class='truncated'>truncated</span>" : "";
  const label = encoding === "base64" ? "(base64)" : "";
  const safeBody = escapeHtml(body);
  return `
    <div class="body-block">
      <div class="body-meta">Body ${label} ${note}</div>
      <textarea id="${id}" class="body-text" rows="10" readonly>${safeBody}</textarea>
    </div>
  `;
};

const escapeHtml = (value) =>
  value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");

const updateFilters = (entries) => {
  const methods = new Set(entries.map((entry) => entry.method));
  const statuses = new Set(entries.map((entry) => entry.status).filter(Boolean));
  setSelectOptions(methodFilter, methods, "All methods");
  setSelectOptions(statusFilter, statuses, "All statuses");
};

const setSelectOptions = (select, values, defaultLabel) => {
  const current = select.value;
  const optionsHtml = `<option value="">${defaultLabel}</option>` + 
    Array.from(values).sort().map(v => `<option value="${v}" ${v === current ? 'selected' : ''}>${v}</option>`).join("");
  
  if (select.innerHTML !== optionsHtml) {
    select.innerHTML = optionsHtml;
  }
};

const applyFilters = (entries) => {
  const search = searchInput.value.trim().toLowerCase();
  const method = methodFilter.value;
  const status = statusFilter.value;
  const startTime = startTimeInput.value ? new Date(startTimeInput.value).getTime() : null;
  const endTime = endTimeInput.value ? new Date(endTimeInput.value).getTime() : null;
  return entries.filter((entry) => {
    if (method && entry.method !== method) return false;
    if (status && String(entry.status) !== status) return false;
    if (startTime || endTime) {
      const entryTime = new Date(entry.startedAt).getTime();
      if (startTime && entryTime < startTime) return false;
      if (endTime && entryTime > endTime) return false;
    }
    if (!search) return true;
    const haystack = [
      entry.url,
      entry.target,
      entry.method,
      entry.status,
      entry.requestBody,
      entry.responseBody,
    ]
      .filter(Boolean)
      .join(" ")
      .toLowerCase();
    return haystack.includes(search);
  });
};

const scheduleAutoRefresh = () => {
  if (refreshTimer) clearInterval(refreshTimer);
  if (autoRefresh.checked) {
    refreshTimer = setInterval(fetchLogs, 2000);
  }
};

refreshButton.addEventListener("click", () => {
  lastLogsJson = ""; // Force re-render
  fetchLogs();
});
searchInput.addEventListener("input", renderList);
methodFilter.addEventListener("change", renderList);
statusFilter.addEventListener("change", renderList);
startTimeInput.addEventListener("change", renderList);
endTimeInput.addEventListener("change", renderList);
autoRefresh.addEventListener("change", scheduleAutoRefresh);

fetchLogs();
scheduleAutoRefresh();
