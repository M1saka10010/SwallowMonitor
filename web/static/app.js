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
  const hosts = await api("/api/hosts");
  const nav = $("#groupNav");
  const root = $("#groupList");
  nav.innerHTML = "";
  root.innerHTML = "";
  cards.clear();
  if (!hosts.length) {
    root.innerHTML = '<p class="empty">暂无主机。</p>';
    return;
  }

  // Group hosts by tag; a host with multiple tags appears under each.
  const groups = new Map();
  const addToGroup = (name, h) => {
    if (!groups.has(name)) groups.set(name, []);
    groups.get(name).push(h);
  };
  for (const h of hosts) {
    const tags = h.tags && h.tags.length ? h.tags : [UNGROUPED];
    for (const t of tags) addToGroup(t, h);
  }

  // Sort group names; keep UNGROUPED last.
  const names = [...groups.keys()].sort((a, b) => {
    if (a === UNGROUPED) return 1;
    if (b === UNGROUPED) return -1;
    return a.localeCompare(b);
  });

  // Render the group navigation only when there is more than one group.
  if (names.length > 1) {
    names.forEach((name, i) => {
      const chip = document.createElement("a");
      chip.className = "group-chip";
      chip.href = "#group-" + i;
      chip.textContent = `${name} (${groups.get(name).length})`;
      chip.onclick = (e) => {
        e.preventDefault();
        $("#group-" + i).scrollIntoView({ behavior: "smooth", block: "start" });
      };
      nav.appendChild(chip);
    });
  }

  names.forEach((name, i) => {
    const section = document.createElement("div");
    section.className = "group";
    section.id = "group-" + i;
    const head = document.createElement("h2");
    head.className = "group-title";
    head.textContent = `${name} (${groups.get(name).length})`;
    section.appendChild(head);

    const grid = document.createElement("div");
    grid.className = "cards";
    for (const h of groups.get(name)) {
      const entry = card(h);
      grid.appendChild(entry.el);
      // A host may render multiple cards (multi-tag); track them all.
      if (!cards.has(h.publicId)) cards.set(h.publicId, []);
      cards.get(h.publicId).push(entry);
    }
    section.appendChild(grid);
    root.appendChild(section);
  });
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

function closeOverviewStream() {
  if (overviewSSE) { overviewSSE.close(); overviewSSE = null; }
}

// ---------- View switching ----------
function showView(id) {
  for (const v of ["overview", "detail", "manage"]) {
    $("#" + v).classList.toggle("hidden", v !== id);
  }
}

// ---------- Detail ----------
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

  const now = Math.floor(Date.now() / 1000);
  const points = await api(`/api/hosts/${publicId}/usage?from=${now - 3600}&to=${now}`) || [];
  initCharts();
  fillCharts(points);
  detailHost = publicId;
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
    data: { labels: [], datasets },
    options: {
      animation: false, responsive: true,
      scales: {
        x: { ticks: { color: muted, maxTicksLimit: 6 }, grid: { color: gridColor } },
        y: { ticks: { color: muted, callback: fmt }, grid: { color: gridColor }, beginAtZero: true },
      },
      plugins: { legend: { labels: { color: textColor } } },
    },
  });
  charts.cpu = mk("cpuChart", [ds("CPU %", "#4cc2ff")], (v) => v + "%");
  charts.mem = mk("memChart", [ds("内存", "#3fb950"), ds("Swap", "#f0883e")], fmtBytes);
  charts.net = mk("netChart", [ds("下行/s", "#4cc2ff"), ds("上行/s", "#f85149")], fmtBytes);
  charts.load = mk("loadChart", [ds("load1", "#4cc2ff"), ds("load5", "#3fb950"), ds("load15", "#f0883e")], (v) => v);
}

function ds(label, color) {
  return { label, data: [], borderColor: color, backgroundColor: color + "33", borderWidth: 1.5, pointRadius: 0, tension: .25, fill: false };
}

function fillCharts(points) {
  for (const p of points) pushPoint(p, false);
  updateAll();
}

function pushPoint(u, update = true) {
  const t = new Date((u.timestamp || 0) * 1000).toLocaleTimeString();
  const cap = 600;
  const add = (c, vals) => {
    c.data.labels.push(t);
    vals.forEach((v, i) => c.data.datasets[i].data.push(v));
    if (c.data.labels.length > cap) {
      c.data.labels.shift();
      c.data.datasets.forEach((d) => d.data.shift());
    }
  };
  add(charts.cpu, [u.cpuUsage || 0]);
  add(charts.mem, [u.memoryUsed || 0, u.swapUsed || 0]);
  add(charts.net, [u.netRecvSpeed || 0, u.netSendSpeed || 0]);
  add(charts.load, [u.load1 || 0, u.load5 || 0, u.load15 || 0]);
  if (update) updateAll();
}

function updateAll() { Object.values(charts).forEach((c) => c.update("none")); }
function destroyCharts() { Object.values(charts).forEach((c) => c.destroy()); charts = {}; }

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
initAuth();
loadOverview().then(openOverviewStream);
