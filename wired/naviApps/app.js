// naviApps — static store: curated apps.json + optional live Flathub search
const grid = document.getElementById("grid");
const statusEl = document.getElementById("status");
const searchEl = document.getElementById("search");
const catEl = document.getElementById("category");
const liveToggle = document.getElementById("liveToggle");

let curated = [];
let liveResults = [];
let liveOn = false;
let activeSource = "";
let liveTimer = null;

async function load() {
  const res = await fetch("apps.json");
  curated = await res.json();
  buildCategories();
  render();
}

function buildCategories() {
  const cats = [...new Set(curated.map((a) => a.category))].sort();
  for (const c of cats) {
    const o = document.createElement("option");
    o.value = c;
    o.textContent = c.toLowerCase();
    catEl.appendChild(o);
  }
}

function badgeFor(a) {
  if (a.source === "apt") return '<span class="badge apt">.deb</span>';
  if (a.source === "flatpak") return '<span class="badge flatpak">flatpak</span>';
  return '<span class="badge webapp">webapp</span>';
}

function matches(a, q, cat) {
  if (activeSource === "navi") {
    if (a.category !== "navi") return false;
  } else if (activeSource && a.source !== activeSource) {
    return false;
  }
  if (cat && a.category !== cat) return false;
  if (!q) return true;
  const hay = `${a.name} ${a.summary} ${a.package} ${a.category}`.toLowerCase();
  return q.toLowerCase().split(/\s+/).every((t) => hay.includes(t));
}

function artFor(a, size) {
  if (a.logo) {
    return `<img class="logo" src="${escapeHTML(a.logo)}" alt="" width="${size}" height="${size}" loading="lazy" onerror="this.outerHTML='${escapeHTML(a.icon || '📦')}'" />`;
  }
  return `<span class="icon">${a.icon || "📦"}</span>`;
}

function cardHTML(a) {
  const live = a._live ? '<span class="badge live">live</span>' : "";
  return `<article class="card">
    <div class="top">${artFor(a, 32)}
    <h2>${escapeHTML(a.name)}</h2></div>
    <div class="badges">${badgeFor(a)}${live}</div>
    <p>${escapeHTML(a.summary)}</p>
    <div class="row">
      <button class="btn primary" data-install="${a.id}">install</button>
      <button class="btn" data-details="${a.id}">details</button>
    </div>
  </article>`;
}

function allApps() {
  return [...curated, ...liveResults];
}

function render() {
  const q = searchEl.value.trim();
  const cat = catEl.value;
  const apps = allApps().filter((a) => matches(a, q, cat));
  grid.innerHTML = apps.map(cardHTML).join("") ||
    `<p class="status">no apps found. try another search — or toggle live flathub.</p>`;
  const liveNote = liveOn ? ` · <span class="live">live flathub on</span>` : "";
  statusEl.innerHTML = `${apps.length} app${apps.length === 1 ? "" : "s"}${liveNote}`;
  wireButtons();
}

function wireButtons() {
  grid.querySelectorAll("[data-details]").forEach((b) =>
    b.addEventListener("click", () => openModal(findApp(b.dataset.details)))
  );
  grid.querySelectorAll("[data-install]").forEach((b) =>
    b.addEventListener("click", () => openModal(findApp(b.dataset.install), true))
  );
}

function findApp(id) {
  return allApps().find((a) => a.id === id);
}

function escapeHTML(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[c]));
}

// --- modal ---
const backdrop = document.getElementById("modalBackdrop");
const mName = document.getElementById("mName");
const mMeta = document.getElementById("mMeta");
const mSummary = document.getElementById("mSummary");
const mCmd = document.getElementById("mCmd");
const mHome = document.getElementById("mHome");
const mDesktopWrap = document.getElementById("mDesktopWrap");
const mDesktop = document.getElementById("mDesktop");

function openModal(a, highlightCopy = false) {
  if (!a) return;
  mName.innerHTML = `${artFor(a, 28)} ${escapeHTML(a.name)}`;
  mMeta.textContent = `${a.source} · ${a.package} · ${a.category}`;
  mSummary.textContent = a.summary;
  mCmd.textContent = a.install;
  if (a.homepage) {
    mHome.hidden = false;
    mHome.href = a.homepage;
  } else {
    mHome.hidden = true;
  }
  const mDownload = document.getElementById("mDownload");
  if (a.downloadUrl) {
    mDownload.hidden = false;
    mDownload.href = a.downloadUrl;
  } else {
    mDownload.hidden = true;
  }
  let removeCmd = "";
  if (a.source === "apt") removeCmd = `sudo apt remove -y ${a.package}`;
  else if (a.source === "flatpak") removeCmd = `flatpak uninstall -y ${a.package}`;
  else if (a.source === "webapp") removeCmd = `./install-webapp.sh --uninstall ${a.package}`;
  const mRemoveWrap = document.getElementById("mRemoveWrap");
  if (removeCmd) {
    mRemoveWrap.hidden = false;
    document.getElementById("mRemove").textContent = removeCmd;
  } else {
    mRemoveWrap.hidden = true;
  }
  if (a.desktopEntry) {
    mDesktopWrap.hidden = false;
    mDesktop.textContent = a.desktopEntry;
  } else {
    mDesktopWrap.hidden = true;
  }
  backdrop.classList.add("open");
  if (highlightCopy) document.getElementById("mCopy").focus();
}

function closeModal() {
  backdrop.classList.remove("open");
}

document.getElementById("mClose").addEventListener("click", closeModal);
backdrop.addEventListener("click", (e) => {
  if (e.target === backdrop) closeModal();
});
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") closeModal();
});
document.getElementById("mCopy").addEventListener("click", async (e) => {
  await copyCmd("mCmd", e.target);
});
document.getElementById("mCopyRemove").addEventListener("click", async (e) => {
  await copyCmd("mRemove", e.target);
});

async function copyCmd(elId, btn) {
  const el = document.getElementById(elId);
  try {
    await navigator.clipboard.writeText(el.textContent);
    btn.textContent = "copied!";
    setTimeout(() => (btn.textContent = "copy"), 1200);
  } catch {
    const r = document.createRange();
    r.selectNodeContents(el);
    getSelection().removeAllRanges();
    getSelection().addRange(r);
    document.execCommand("copy");
  }
}

// --- filters ---
document.querySelectorAll(".filters button[data-source]").forEach((b) =>
  b.addEventListener("click", () => {
    document.querySelectorAll(".filters button[data-source]").forEach((x) => x.classList.remove("active"));
    b.classList.add("active");
    activeSource = b.dataset.source;
    render();
  })
);

searchEl.addEventListener("input", () => {
  render();
  scheduleLive();
});
catEl.addEventListener("change", render);

liveToggle.addEventListener("click", () => {
  liveOn = !liveOn;
  liveToggle.textContent = liveOn ? "✦ live flathub: on" : "✦ live flathub: off";
  liveToggle.classList.toggle("active", liveOn);
  if (liveOn) doLiveSearch();
  else { liveResults = []; render(); }
});

function scheduleLive() {
  if (!liveOn) return;
  clearTimeout(liveTimer);
  liveTimer = setTimeout(doLiveSearch, 400);
}

async function doLiveSearch() {
  const q = searchEl.value.trim();
  if (!q) { liveResults = []; render(); return; }
  statusEl.innerHTML = `searching flathub for “${escapeHTML(q)}”…`;
  try {
    // Flathub API v2 (CORS-enabled)
    const res = await fetch(`https://flathub.org/api/v2/search/${encodeURIComponent(q)}?page_size=12`);
    if (!res.ok) throw new Error(res.status);
    const data = await res.json();
    const hits = data.hits || data.results || [];
    liveResults = hits.map((h) => ({
      id: `live-${h.app_id || h.id}`,
      name: h.name || h.app_id,
      summary: h.summary || h.description || "from Flathub",
      category: (h.main_category || "Flatpak"),
      source: "flatpak",
      package: h.app_id || h.id,
      install: `flatpak install -y flathub ${h.app_id || h.id}`,
      homepage: `https://flathub.org/apps/${h.app_id || h.id}`,
      icon: "📦",
      _live: true,
    }));
  } catch {
    statusEl.textContent = "live search failed (offline?) — showing curated apps.";
    liveResults = [];
  }
  render();
}

// --- sticky glass header: compact mode on scroll ---
// Hysteresis (add past 72px, drop below 24px) + rAF throttle so the
// header-height change can't flap the toggle when settling at the top.
const heroEl = document.querySelector("header.hero");
if (heroEl) {
  const ADD_AT = 72;
  const REMOVE_AT = 24;
  let ticking = false;
  const update = () => {
    ticking = false;
    const y = window.scrollY;
    if (y > ADD_AT) heroEl.classList.add("scrolled");
    else if (y < REMOVE_AT) heroEl.classList.remove("scrolled");
  };
  window.addEventListener("scroll", () => {
    if (!ticking) {
      ticking = true;
      requestAnimationFrame(update);
    }
  }, { passive: true });
  update();
}

load();
