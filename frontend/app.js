const API = "http://localhost:8000";

const PATH_CHALLENGES = "/challenges";
const PATH_LIST       = "/instances";
const PATH_CREATE     = "/instance/create";
const PATH_UPDATE     = "/instance/update-status";
const PATH_RENEW      = "/instance/renew";

let workers = [];

function rememberWorker(workerId) {
  workers.push(workerId || "?");
  if (workers.length > 20) workers.shift();
}

async function send(method, path, body) {
  const opts = { method: method };
  if (body) {
    opts.headers = { "Content-Type": "application/json" };
    opts.body = JSON.stringify(body);
  }
  try {
    const res = await fetch(API + path, opts);
    const workerId = res.headers.get("X-Worker-ID");
    const text = await res.text();
    rememberWorker(workerId);
    showToast(method + " " + path, res.status, workerId, res.ok);
    return { ok: res.ok, status: res.status, text: text };
  } catch (e) {
    rememberWorker("ERR");
    showToast(method + " " + path, "ERR", null, false);
    return { ok: false, status: 0, text: "FAILED: " + e };
  }
}

async function load(path) {
  try {
    const res = await fetch(API + path);
    rememberWorker(res.headers.get("X-Worker-ID"));
    if (res.ok) return await res.json();
  } catch (e) {
    rememberWorker("ERR");
  }
  return null;
}

function showToast(action, status, workerId, ok) {
  let box = document.getElementById("toasts");
  if (!box) {
    box = document.createElement("div");
    box.id = "toasts";
    box.className = "fixed bottom-4 right-4 left-4 sm:left-auto z-50 flex flex-col items-end gap-2";
    document.body.appendChild(box);
  }

  const toast = document.createElement("div");
  const border = ok ? "border-primary" : "border-danger";
  toast.className = "flex items-center gap-2 px-3 py-2 rounded border bg-dark-sidebar text-body transition-opacity duration-500 " + border;
  toast.innerHTML =
    '<i data-lucide="server" class="icon-em ' + (ok ? "text-primary" : "text-danger") + '"></i>' +
    "<span>worker " + escapeHTML(workerId || "?") + "</span>" +
    '<span class="text-text-secondary">' + escapeHTML(action) + " &middot; " + status + "</span>";
  box.appendChild(toast);
  drawIcons();

  setTimeout(function () { toast.style.opacity = "0"; }, 3000);
  setTimeout(function () { toast.remove(); }, 3500);
}

function escapeHTML(text) {
  return String(text).replace(/[&<>"']/g, function (c) { return "&#" + c.charCodeAt(0) + ";"; });
}

function timeLeft(expiresAt) {
  const seconds = Math.max(0, Math.round((new Date(expiresAt) - Date.now()) / 1000));
  return Math.floor(seconds / 60) + "m " + (seconds % 60) + "s";
}

function tick() {
  document.querySelectorAll("[data-expires]").forEach(function (el) {
    el.textContent = timeLeft(el.dataset.expires);
  });
}
setInterval(tick, 1000);

function drawIcons() {
  if (window.lucide) lucide.createIcons();
}
