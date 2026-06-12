"use strict";

const $ = (sel) => document.querySelector(sel);
const fmtBytes = (n) => {
  if (!n) return "0 B";
  const u = ["B", "KB", "MB", "GB", "TB", "PB"];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return n.toFixed(i ? 1 : 0) + " " + u[i];
};
const pct = (used, total) => (total ? (used / total) * 100 : 0);

let charts = {};
let overviewSSE = null;
let detailHost = null;
let canManage = false;
let detailLastTs = 0;
let detailSampleInterval = 60;
let zoomChart = null;
let memoryTotalText = "";
let overviewHosts = [];
let overviewFilter = "";
// publicId -> { el, refs, tags } for incremental overview updates.
const cards = new Map();

const UNGROUPED = "未分组";

async function api(path, opts) {
  const res = await fetch(path, opts);
  if (!res.ok) throw new Error(await res.text());
  if (res.status === 204) return null;
  return res.json();
}

// ---------- Overview ----------
async function loadOverview() {
  overviewHosts = await api("/api/hosts");
  renderOverview();
}

function renderOverview() {
  const nav = $("#groupNav");
  const root = $("#groupList");
  nav.innerHTML = "";
  root.innerHTML = "";
  cards.clear();
  if (!overviewHosts.length) {
    root.innerHTML = '<p class="empty">暂无主机。</p>';
    return;
  }

  // Sort group names; keep UNGROUPED last.
  const tagCounts = new Map();
  for (const h of overviewHosts) {
    const tags = h.tags && h.tags.length ? h.tags : [UNGROUPED];
    for (const t of tags) tagCounts.set(t, (tagCounts.get(t) || 0) + 1);
  }
  const names = [...tagCounts.keys()].sort((a, b) => {
    if (a === UNGROUPED) return 1;
    if (b === UNGROUPED) return -1;
    return a.localeCompare(b);
  });

  const addFilterChip = (name, count) => {
    const chip = document.createElement("button");
    chip.type = "button";
    chip.className = "group-chip" + (overviewFilter === name ? " active" : "");
    chip.textContent = `${name || "全部"} (${count})`;
    chip.onclick = () => {
      overviewFilter = name;
      renderOverview();
    };
    nav.appendChild(chip);
  };
  addFilterChip("", overviewHosts.length);
  names.forEach((name) => addFilterChip(name, tagCounts.get(name)));

  const visibleHosts = overviewFilter
    ? overviewHosts.filter((h) => {
      const tags = h.tags && h.tags.length ? h.tags : [UNGROUPED];
      return tags.includes(overviewFilter);
    })
    : overviewHosts;

  const grid = document.createElement("div");
  grid.className = "cards";
  for (const h of visibleHosts) {
    const entry = card(h);
    grid.appendChild(entry.el);
    cards.set(h.publicId, [entry]);
  }
  root.appendChild(grid);
}

function card(h) {
  const el = document.createElement("div");
  el.className = "card";
  el.onclick = () => openDetail(h.publicId);
  el.innerHTML = `
    <div class="card-head">
      <span class="name"></span>
      <span class="status"><span class="dot"></span><span class="status-text"></span></span>
    </div>
    <div class="meta"></div>
    <div class="tags"></div>
    ${barTpl("CPU")}
    ${barTpl("内存")}
    ${barTpl("磁盘")}
    <div class="net">
      <span class="net-recv"></span>
      <span class="net-send"></span>
    </div>`;

  const bars = el.querySelectorAll(".bar");
  const refs = {
    name: el.querySelector(".name"),
    dot: el.querySelector(".dot"),
    statusText: el.querySelector(".status-text"),
    meta: el.querySelector(".meta"),
    cpu: barRefs(bars[0]),
    mem: barRefs(bars[1]),
    disk: barRefs(bars[2]),
    netRecv: el.querySelector(".net-recv"),
    netSend: el.querySelector(".net-send"),
  };

  refs.name.textContent = h.nickname;
  refs.meta.textContent = `${h.hostname || "-"} · ${h.os || "?"} ${h.platformVersion || ""}`;
  const tagsEl = el.querySelector(".tags");
  for (const t of h.tags || []) {
    const span = document.createElement("span");
    span.className = "tag";
    span.textContent = t;
    tagsEl.appendChild(span);
  }
  const entry = { el, refs };
  applyStatus(entry, h.online);
  applyUsage(entry, h.latest || {});
  return entry;
}

function barTpl(label) {
  return `<div class="bar">
    <div class="bar-label"><span>${label}</span><span class="bar-text"></span></div>
    <div class="bar-track"><div class="bar-fill"></div></div>
  </div>`;
}

function barRefs(barEl) {
  return { text: barEl.querySelector(".bar-text"), fill: barEl.querySelector(".bar-fill") };
}

function setBar(ref, text, value) {
  ref.text.textContent = text;
  ref.fill.style.width = Math.min(100, value).toFixed(1) + "%";
}

function applyUsage(entry, u) {
  const r = entry.refs;
  setBar(r.cpu, (u.cpuUsage || 0).toFixed(1) + "%", u.cpuUsage || 0);
  setBar(r.mem, fmtBytes(u.memoryUsed) + " / " + fmtBytes(u.memoryTotal), pct(u.memoryUsed, u.memoryTotal));
  setBar(r.disk, fmtBytes(u.diskUsed) + " / " + fmtBytes(u.diskTotal), pct(u.diskUsed, u.diskTotal));
  r.netRecv.innerHTML = "&darr; " + fmtBytes(u.netRecvSpeed || 0) + "/s";
  r.netSend.innerHTML = "&uarr; " + fmtBytes(u.netSendSpeed || 0) + "/s";
}

function applyStatus(entry, online) {
  const r = entry.refs;
  r.dot.className = "dot " + (online ? "online" : "offline");
  r.statusText.textContent = online ? "在线" : "离线";
}

// ---------- Overview SSE ----------
function openOverviewStream() {
  if (overviewSSE) return;
  overviewSSE = new EventSource("/events");
  overviewSSE.onmessage = (ev) => {
    const msg = JSON.parse(ev.data);
    // Feed the detail chart if this host's detail page is open.
    if (msg.type === "usage" && detailHost && msg.publicId === detailHost) {
      pushPoint(msg.data);
    }
    const entries = cards.get(msg.publicId);
    if (!entries) {
      // New host reporting that isn't rendered yet: rebuild the list.
      loadOverview();
      return;
    }
    for (const entry of entries) {
      if (msg.type === "usage") {
        applyUsage(entry, msg.data);
        applyStatus(entry, true);
      } else if (msg.type === "status") {
        applyStatus(entry, msg.online);
      }
    }
  };
}

// ---------- View switching ----------
function showView(id) {
  for (const v of ["overview", "detail", "manage"]) {
    $("#" + v).classList.toggle("hidden", v !== id);
  }
}

// ---------- Detail ----------
const RANGES = [
  { label: "5 分钟", seconds: 300 },
  { label: "1 小时", seconds: 3600 },
  { label: "3 小时", seconds: 10800 },
  { label: "1 天", seconds: 86400 },
  { label: "7 天", seconds: 604800 },
];
let rangeSeconds = 3600; // default 1h

async function openDetail(publicId) {
  const h = await api("/api/hosts/" + publicId);
  showView("detail");
  $("#detailName").textContent = h.nickname;

  const info = [
    ["主机名", h.hostname], ["系统", `${h.os} ${h.platform} ${h.platformVersion}`],
    ["内核", h.kernelArch], ["CPU", h.modelName], ["核心数", h.cores],
    ["虚拟化", h.virtualizationRole || "-"], ["HostID", h.hostId || "-"],
  ];
  $("#sysinfo").innerHTML = info.map(([k, v]) => `<div><span>${k}</span>${escapeHtml(String(v ?? "-"))}</div>`).join("");

  detailHost = publicId;
  renderRangeNav();
  initCharts();
  await loadRange();
}

function renderRangeNav() {
  const nav = $("#rangeNav");
  nav.innerHTML = "";
  for (const r of RANGES) {
    const btn = document.createElement("button");
    btn.className = "range-chip" + (r.seconds === rangeSeconds ? " active" : "");
    btn.textContent = r.label;
    btn.onclick = () => {
      if (r.seconds === rangeSeconds) return;
      rangeSeconds = r.seconds;
      nav.querySelectorAll(".range-chip").forEach((c) => c.classList.remove("active"));
      btn.classList.add("active");
      loadRange();
    };
    nav.appendChild(btn);
  }
}

async function loadRange() {
  if (!detailHost) return;
  const now = Math.floor(Date.now() / 1000);
  const from = now - rangeSeconds;
  const points = await api(`/api/hosts/${detailHost}/usage?from=${now - rangeSeconds}&to=${now}`) || [];
  detailSampleInterval = estimateSampleInterval(points);
  clearCharts();
  setMemoryTotal(0);
  setChartWindow(from, now);
  if (!points.length) {
    appendPoint(from, zeroUsage(from));
    appendPoint(now, zeroUsage(now));
    updateAll();
    return;
  }

  setMemoryTotal(points[points.length - 1].memoryTotal || 0);
  const firstTs = points[0].timestamp || 0;
  const gapThreshold = Math.max(detailSampleInterval * 3, 60);
  if (firstTs && firstTs - from > gapThreshold) {
    appendPoint(from, zeroUsage(from));
    appendOfflineSegment(from, firstTs);
  }
  for (const p of points) pushPoint(p, false);
  appendTrailingOffline(now);
  updateAll();
}

function estimateSampleInterval(points) {
  const deltas = [];
  for (let i = 1; i < points.length; i++) {
    const prev = points[i - 1].timestamp || 0;
    const next = points[i].timestamp || 0;
    const delta = next - prev;
    if (delta > 0) deltas.push(delta);
  }
  if (!deltas.length) return 60;
  deltas.sort((a, b) => a - b);
  // Use a lower percentile and cap it. A single huge gap should not become the
  // "normal" interval, otherwise offline periods would still be connected.
  return Math.min(300, Math.max(1, deltas[Math.floor(deltas.length * 0.25)]));
}

function backToOverview() {
  detailHost = null;
  showView("overview");
  loadOverview();
}

function initCharts() {
  destroyCharts();
  const cssVar = (name) => getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  const muted = cssVar("--muted");
  const gridColor = cssVar("--border");
  const textColor = cssVar("--text");
  const mk = (id, datasets, fmt) => new Chart($("#" + id), {
    type: "line",
    data: { datasets },
    options: {
      animation: false, responsive: true,
      scales: {
        x: {
          type: "linear",
          ticks: {
            color: muted,
            maxTicksLimit: 6,
            callback: (value) => formatTs(Math.floor(value / 1000)),
          },
          grid: { color: gridColor },
        },
        y: { ticks: { color: muted, callback: fmt }, grid: { color: gridColor }, beginAtZero: true },
      },
      plugins: { legend: { labels: { color: textColor } } },
    },
  });
  charts.cpu = mk("cpuChart", [ds("CPU %", "#4cc2ff"), offlineDs()], (v) => v + "%");
  charts.mem = mk("memChart", [ds("内存", "#3fb950"), ds("Swap", "#f0883e"), offlineDs()], fmtBytes);
  charts.net = mk("netChart", [ds("下行/s", "#4cc2ff"), ds("上行/s", "#f85149"), offlineDs()], fmtBytes);
  charts.load = mk("loadChart", [ds("load1", "#4cc2ff"), ds("load5", "#3fb950"), ds("load15", "#f0883e"), offlineDs()], (v) => v);
}

function chartOptions(fmt) {
  const cssVar = (name) => getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  const muted = cssVar("--muted");
  const gridColor = cssVar("--border");
  const textColor = cssVar("--text");
  return {
    animation: false, responsive: true, maintainAspectRatio: false,
    scales: {
      x: {
        type: "linear",
        ticks: {
          color: muted,
          maxTicksLimit: 6,
          callback: (value) => formatTs(Math.floor(value / 1000)),
        },
        grid: { color: gridColor },
      },
      y: { ticks: { color: muted, callback: fmt }, grid: { color: gridColor }, beginAtZero: true },
    },
    plugins: { legend: { labels: { color: textColor } } },
  };
}

function chartMeta(key) {
  return {
    cpu: { title: "CPU", fmt: (v) => v + "%" },
    mem: { title: "内存", fmt: fmtBytes },
    net: { title: "网络", fmt: fmtBytes },
    load: { title: "负载", fmt: (v) => v },
  }[key];
}

function setMemoryTotal(total) {
  memoryTotalText = total ? `总内存 ${fmtBytes(total)}` : "";
  const el = $("#memTotal");
  el.textContent = memoryTotalText;
  el.classList.toggle("hidden", !memoryTotalText);
}

function cloneChartData(chart) {
  return {
    datasets: chart.data.datasets.map((d) => ({
      ...d,
      data: d.data.map((p) => ({ ...p })),
    })),
  };
}

function openChartModal(key, title) {
  const source = charts[key];
  const meta = chartMeta(key);
  if (!source || !meta) return;
  closeChartModal();
  $("#chartModalTitle").textContent = key === "mem" && memoryTotalText ? `${title || meta.title} · ${memoryTotalText}` : title || meta.title;
  $("#chartModal").classList.remove("hidden");
  zoomChart = new Chart($("#zoomChart"), {
    type: source.config.type,
    data: cloneChartData(source),
    options: chartOptions(meta.fmt),
  });
  zoomChart.options.scales.x.min = source.options.scales.x.min;
  zoomChart.options.scales.x.max = source.options.scales.x.max;
  zoomChart.update("none");
}

function closeChartModal() {
  if (zoomChart) {
    zoomChart.destroy();
    zoomChart = null;
  }
  $("#chartModal").classList.add("hidden");
}

function initChartModal() {
  document.querySelectorAll(".chart-box[data-chart]").forEach((box) => {
    box.tabIndex = 0;
    box.title = "点击放大";
    box.addEventListener("click", () => openChartModal(box.dataset.chart, box.dataset.title));
    box.addEventListener("keydown", (e) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        openChartModal(box.dataset.chart, box.dataset.title);
      }
    });
  });
  $("#chartModalClose").onclick = closeChartModal;
  $("#chartModal").addEventListener("click", (e) => {
    if (e.target.id === "chartModal") closeChartModal();
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && !$("#chartModal").classList.contains("hidden")) closeChartModal();
  });
}

function ds(label, color) {
  return { label, data: [], borderColor: color, backgroundColor: color + "33", borderWidth: 1.5, pointRadius: 0, tension: 0, fill: false, parsing: false, spanGaps: false };
}

function offlineDs() {
  return { label: "离线", data: [], borderColor: "#8b949e", backgroundColor: "#8b949e33", borderWidth: 1.5, pointRadius: 0, tension: 0, fill: false, parsing: false, spanGaps: false };
}

function formatTs(ts) {
  const d = new Date(ts * 1000);
  // For ranges spanning a day or more, include the date so labels aren't ambiguous.
  if (rangeSeconds >= 86400) {
    return d.toLocaleString([], { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
  }
  return d.toLocaleTimeString();
}

function pushPoint(u, update = true) {
  const ts = u.timestamp || 0;
  if (!ts) return;

  const gapThreshold = Math.max(detailSampleInterval * 3, 60);
  if (detailLastTs && ts - detailLastTs > gapThreshold) {
    appendOfflineSegment(detailLastTs, ts);
  }

  appendPoint(ts, u);
  if (u.memoryTotal) setMemoryTotal(u.memoryTotal);
  detailLastTs = ts;
  if (update) {
    setChartWindow(ts - rangeSeconds, ts);
    updateAll();
  }
}

function appendTrailingOffline(now) {
  if (!detailLastTs) return;
  const gapThreshold = Math.max(detailSampleInterval * 3, 60);
  if (now - detailLastTs <= gapThreshold) return;
  appendOfflineSegment(detailLastTs, now);
  appendPoint(now, zeroUsage(now));
  detailLastTs = now;
}

function setChartWindow(fromTs, toTs) {
  for (const c of Object.values(charts)) {
    c.options.scales.x.min = fromTs * 1000;
    c.options.scales.x.max = toTs * 1000;
  }
}

function appendOfflineSegment(fromTs, toTs) {
  const start = fromTs + detailSampleInterval;
  const end = toTs - detailSampleInterval;
  if (end <= start) {
    appendPoint(fromTs + 1, zeroUsage(fromTs + 1));
    appendPoint(Math.max(fromTs + 2, toTs - 1), zeroUsage(Math.max(fromTs + 2, toTs - 1)));
    return;
  }
  appendPoint(start, zeroUsage(start));
  appendPoint(end, zeroUsage(end));
}

function zeroUsage(ts) {
  return {
    timestamp: ts,
    offline: true,
    cpuUsage: 0,
    memoryUsed: 0,
    swapUsed: 0,
    diskUsed: 0,
    netRecvSpeed: 0,
    netSendSpeed: 0,
    load1: 0,
    load5: 0,
    load15: 0,
  };
}

function appendPoint(ts, u) {
  const x = ts * 1000;
  const add = (c, vals) => {
    if (!c._ts) c._ts = [];
    c._ts.push(ts);
    vals.forEach((v, i) => c.data.datasets[i].data.push({ x, y: u.offline ? null : v }));
    // Last dataset is the gray offline segment. Keep normal series broken
    // during offline periods and render only this gray 0-value line.
    c.data.datasets[c.data.datasets.length - 1].data.push({ x, y: u.offline ? 0 : null });
    // Drop points older than the selected window (keep a small margin).
    const cutoff = ts - rangeSeconds;
    while (c._ts.length && c._ts[0] < cutoff) {
      c._ts.shift();
      c.data.datasets.forEach((d) => d.data.shift());
    }
  };
  add(charts.cpu, [u.cpuUsage || 0]);
  add(charts.mem, [u.memoryUsed || 0, u.swapUsed || 0]);
  add(charts.net, [u.netRecvSpeed || 0, u.netSendSpeed || 0]);
  add(charts.load, [u.load1 || 0, u.load5 || 0, u.load15 || 0]);
}

function clearCharts() {
  detailLastTs = 0;
  for (const c of Object.values(charts)) {
    c._ts = [];
    c.data.datasets.forEach((d) => (d.data.length = 0));
  }
}

function updateAll() { Object.values(charts).forEach((c) => c.update("none")); }
function destroyCharts() { Object.values(charts).forEach((c) => c.destroy()); charts = {}; closeChartModal(); }

// ---------- Manage ----------
function openManage() {
  if (!canManage) return;
  detailHost = null;
  showView("manage");
  $("#newTokenOut").classList.add("hidden");
  loadHostTable();
}

async function loadHostTable() {
  const hosts = await api("/api/hosts");
  const tb = $("#hostTable tbody");
  tb.innerHTML = "";
  for (const h of hosts) tb.appendChild(hostRow(h));
}

function hostRow(h) {
  const tr = document.createElement("tr");
  tr.dataset.id = h.publicId;
  renderRowView(tr, h);
  return tr;
}

function renderRowView(tr, h) {
  const online = h.online;
  tr.innerHTML = `
    <td class="cell-nick"></td>
    <td class="cell-tags"></td>
    <td class="cell-id"></td>
    <td><span class="status"><span class="dot ${online ? "online" : "offline"}"></span>${online ? "在线" : "离线"}</span></td>
    <td><span class="install-link">安装命令</span> <span class="edit">编辑</span> <span class="del">删除</span></td>`;
  tr.querySelector(".cell-nick").textContent = h.nickname;
  tr.querySelector(".cell-id").textContent = h.publicId;
  tr.querySelector(".cell-id").style.color = "var(--muted)";
  const tagsCell = tr.querySelector(".cell-tags");
  if (h.tags && h.tags.length) {
    for (const t of h.tags) {
      const span = document.createElement("span");
      span.className = "tag";
      span.textContent = t;
      tagsCell.appendChild(span);
    }
  } else {
    tagsCell.innerHTML = '<span class="muted">未分组</span>';
  }
  tr.querySelector(".install-link").onclick = () => toggleInstallRow(tr, h);
  tr.querySelector(".edit").onclick = () => { removeInstallRow(tr); renderRowEdit(tr, h); };
  tr.querySelector(".del").onclick = async () => {
    if (!confirm("删除主机「" + h.nickname + "」及其历史数据？")) return;
    await api("/api/hosts/" + h.publicId, { method: "DELETE" });
    removeInstallRow(tr);
    tr.remove();
    loadOverview();
  };
}

function removeInstallRow(tr) {
  if (tr.nextElementSibling && tr.nextElementSibling.classList.contains("install-row")) {
    tr.nextElementSibling.remove();
  }
}

function toggleInstallRow(tr, h) {
  if (tr.nextElementSibling && tr.nextElementSibling.classList.contains("install-row")) {
    removeInstallRow(tr);
    return;
  }
  const token = h.token || "";
  const cmd = token ? installCommand(token) : "";
  const row = document.createElement("tr");
  row.className = "install-row";
  const td = document.createElement("td");
  td.colSpan = 5;
  td.innerHTML = `
    <div class="install-out">
      <div class="copy-row">
        <span class="copy-label">Token</span>
        <code class="row-token"></code>
        <button type="button" class="copy-btn" data-copy="token">复制</button>
      </div>
      <div class="copy-row">
        <span class="copy-label">安装命令</span>
        <code class="row-cmd"></code>
        <button type="button" class="copy-btn" data-copy="cmd">复制</button>
      </div>
    </div>`;
  td.querySelector(".row-token").textContent = token;
  td.querySelector(".row-cmd").textContent = cmd;
  td.querySelectorAll(".copy-btn").forEach((btn) => {
    btn.onclick = () => copyText(btn.dataset.copy === "token" ? token : cmd, btn);
  });
  row.appendChild(td);
  tr.after(row);
}

function renderRowEdit(tr, h) {
  tr.innerHTML = `
    <td><input class="edit-nick" value="" /></td>
    <td><input class="edit-tags" placeholder="分组标签，逗号分隔" value="" /></td>
    <td class="cell-id"></td>
    <td></td>
    <td><span class="save">保存</span> <span class="cancel">取消</span></td>`;
  tr.querySelector(".edit-nick").value = h.nickname;
  tr.querySelector(".edit-tags").value = (h.tags || []).join(", ");
  tr.querySelector(".cell-id").textContent = h.publicId;
  tr.querySelector(".cell-id").style.color = "var(--muted)";

  const save = async () => {
    const nickname = tr.querySelector(".edit-nick").value.trim();
    const tags = parseTags(tr.querySelector(".edit-tags").value);
    if (!nickname) { alert("昵称不能为空"); return; }
    try {
      await api("/api/hosts/" + h.publicId, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ nickname, tags }),
      });
      renderRowView(tr, { ...h, nickname, tags });
      loadOverview();
    } catch (e) {
      alert("更新失败：" + e.message);
    }
  };
  tr.querySelector(".save").onclick = save;
  tr.querySelector(".cancel").onclick = () => renderRowView(tr, h);
  tr.querySelector(".edit-nick").focus();
}

function parseTags(s) {
  return (s || "").split(",").map((t) => t.trim()).filter(Boolean);
}

async function addHost(e) {
  e.preventDefault();
  const nickname = $("#newNick").value.trim();
  const token = $("#newToken").value.trim();
  const tags = parseTags($("#newTags").value);
  const h = await api("/api/hosts", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ nickname, token, tags }),
  });
  showInstall(h.token);
  $("#newNick").value = "";
  $("#newToken").value = "";
  $("#newTags").value = "";
  loadHostTable();
  loadOverview();
}

// reportUrl derives the agent report endpoint from the current page location.
function reportUrl() {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  return `${proto}://${location.host}/report`;
}

function installCommand(token) {
  return `curl -fsSL https://raw.githubusercontent.com/M1saka10010/SwallowAgent/main/install.sh | bash -s -- ${reportUrl()} ${token}`;
}

function showInstall(token) {
  $("#tokenValue").textContent = token;
  $("#installCmd").textContent = installCommand(token);
  $("#newTokenOut").classList.remove("hidden");
}

async function copyText(text, btn) {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
    } else {
      // Fallback for non-secure contexts (e.g. plain http LAN access).
      const ta = document.createElement("textarea");
      ta.value = text;
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      document.execCommand("copy");
      document.body.removeChild(ta);
    }
    const old = btn.textContent;
    btn.textContent = "已复制";
    setTimeout(() => { btn.textContent = old; }, 1500);
  } catch {
    alert("复制失败，请手动复制。");
  }
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

// ---------- Theme ----------
const THEME_LABELS = { auto: "跟随系统", light: "浅色", dark: "深色" };
const THEME_ORDER = ["auto", "light", "dark"];

function currentTheme() {
  const t = localStorage.getItem("theme");
  return t === "light" || t === "dark" ? t : "auto";
}

function applyTheme(theme) {
  const root = document.documentElement;
  if (theme === "auto") {
    root.removeAttribute("data-theme");
    localStorage.removeItem("theme");
  } else {
    root.setAttribute("data-theme", theme);
    localStorage.setItem("theme", theme);
  }
  $("#themeBtn").textContent = THEME_LABELS[theme];
  // Re-theme open charts so axis/grid colors match the new palette.
  if (detailHost) refreshChartTheme();
}

function cycleTheme() {
  const next = THEME_ORDER[(THEME_ORDER.indexOf(currentTheme()) + 1) % THEME_ORDER.length];
  applyTheme(next);
}

function refreshChartTheme() {
  const cssVar = (n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim();
  const muted = cssVar("--muted"), grid = cssVar("--border"), text = cssVar("--text");
  for (const c of Object.values(charts)) {
    c.options.scales.x.ticks.color = muted;
    c.options.scales.y.ticks.color = muted;
    c.options.scales.x.grid.color = grid;
    c.options.scales.y.grid.color = grid;
    c.options.plugins.legend.labels.color = text;
    c.update("none");
  }
}

function initTheme() {
  applyTheme(currentTheme());
  // Follow system changes while in auto mode.
  window.matchMedia("(prefers-color-scheme: light)").addEventListener("change", () => {
    if (currentTheme() === "auto" && detailHost) refreshChartTheme();
  });
  $("#themeBtn").onclick = cycleTheme;
}

// ---------- Wiring ----------
$("#manageBtn").onclick = openManage;
$("#manageBackBtn").onclick = backToOverview;
$("#homeLink").onclick = backToOverview;
$("#backBtn").onclick = backToOverview;
$("#addForm").onsubmit = addHost;

// Copy buttons inside the install block (event delegation).
$("#newTokenOut").addEventListener("click", (e) => {
  const btn = e.target.closest(".copy-btn");
  if (!btn) return;
  const el = document.getElementById(btn.dataset.copy);
  if (el) copyText(el.textContent, btn);
});

async function initAuth() {
  try {
    const me = await api("/api/me");
    canManage = !!me.loggedIn;
    // Show manage button only when the user can manage.
    $("#manageBtn").classList.toggle("hidden", !canManage);
    // Show login/logout based on whether auth is enabled and current state.
    if (me.authEnabled) {
      $("#loginLink").classList.toggle("hidden", canManage);
      $("#logoutLink").classList.toggle("hidden", !canManage);
    }
  } catch {
    canManage = false;
  }
}

initTheme();
initChartModal();
initAuth();
loadOverview().then(openOverviewStream);
