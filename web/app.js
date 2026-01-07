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

const fetchLogs = async () => {
  const response = await fetch("/api/logs");
  if (!response.ok) {
    logList.innerHTML = "<p class='error'>Failed to fetch logs.</p>";
    return;
  }
  logs = await response.json();
  updateFilters(logs);
  renderList();
  if (selectedId) {
    const match = logs.find((entry) => entry.id === selectedId);
    if (match) {
      renderDetails(match);
    }
  }
};

const renderList = () => {
  const filtered = applyFilters(logs);
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
        <span class="status">${entry.status || "–"}</span>
      </div>
      <div class="log-entry__url">${entry.url}</div>
      <div class="log-entry__target">${entry.target || "No target"}</div>
      <div class="log-entry__time">${new Date(entry.startedAt).toLocaleTimeString()}</div>
    `;
    item.addEventListener("click", () => {
      selectedId = entry.id;
      renderDetails(entry);
      renderList();
    });
    logList.appendChild(item);
  });
};

const renderDetails = (entry) => {
  details.innerHTML = `
    <div class="detail-header">
      <h2>${entry.method} ${entry.url}</h2>
      <p>Target: <span>${entry.target || "Not resolved"}</span></p>
      <p>Status: <strong>${entry.status || "Pending"}</strong> • Duration: ${entry.durationMillis} ms</p>
    </div>
    <div class="detail-grid">
      <div>
        <h3>Request</h3>
        <p><strong>Client:</strong> ${entry.clientIp || ""}</p>
        <p><strong>Content-Type:</strong> ${entry.requestContentType || ""}</p>
        <p><strong>Content-Length:</strong> ${entry.requestContentLength || 0}</p>
        ${renderHeaderTable(entry.requestHeaders)}
        ${renderBody(entry.requestBody, entry.requestBodyEncoding, entry.requestBodyTruncated)}
      </div>
      <div>
        <h3>Response</h3>
        <p><strong>Content-Type:</strong> ${entry.responseContentType || ""}</p>
        <p><strong>Content-Length:</strong> ${entry.responseContentLength || 0}</p>
        ${renderHeaderTable(entry.responseHeaders)}
        ${renderBody(entry.responseBody, entry.responseBodyEncoding, entry.responseBodyTruncated)}
      </div>
    </div>
    ${entry.error ? `<div class="error-box">Error: ${entry.error}</div>` : ""}
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

const renderBody = (body, encoding, truncated) => {
  if (!body) {
    return "<p class='placeholder'>No body captured.</p>";
  }
  const note = truncated ? "<span class='truncated'>truncated</span>" : "";
  const label = encoding === "base64" ? "(base64)" : "";
  return `
    <div class="body-block">
      <div class="body-meta">Body ${label} ${note}</div>
      <pre>${escapeHtml(body)}</pre>
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
  select.innerHTML = `<option value="">${defaultLabel}</option>`;
  Array.from(values)
    .sort()
    .forEach((value) => {
      const option = document.createElement("option");
      option.value = value;
      option.textContent = value;
      select.appendChild(option);
    });
  select.value = current;
};

const applyFilters = (entries) => {
  const search = searchInput.value.trim().toLowerCase();
  const method = methodFilter.value;
  const status = statusFilter.value;
  const startTime = startTimeInput.value ? new Date(startTimeInput.value).getTime() : null;
  const endTime = endTimeInput.value ? new Date(endTimeInput.value).getTime() : null;
  return entries.filter((entry) => {
    if (method && entry.method !== method) {
      return false;
    }
    if (status && String(entry.status) !== status) {
      return false;
    }
    if (startTime || endTime) {
      const entryTime = new Date(entry.startedAt).getTime();
      if (startTime && entryTime < startTime) {
        return false;
      }
      if (endTime && entryTime > endTime) {
        return false;
      }
    }
    if (!search) {
      return true;
    }
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
  if (refreshTimer) {
    clearInterval(refreshTimer);
  }
  if (autoRefresh.checked) {
    refreshTimer = setInterval(fetchLogs, 2000);
  }
};

refreshButton.addEventListener("click", fetchLogs);
searchInput.addEventListener("input", renderList);
methodFilter.addEventListener("change", renderList);
statusFilter.addEventListener("change", renderList);
startTimeInput.addEventListener("change", renderList);
endTimeInput.addEventListener("change", renderList);
autoRefresh.addEventListener("change", scheduleAutoRefresh);

fetchLogs();
scheduleAutoRefresh();
