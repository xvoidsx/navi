// naviApps — static local store: the curated webapp catalog ships inlined
// as catalog.js (generated from apps.json by build-catalog.sh), so the page
// runs offline from file:// with no server and no browser flags.
// Runs offline from the deployed tree; install via `navi-webapp <name>`.
const grid = document.getElementById("grid");
const naviGrid = document.getElementById("naviGrid");
const naviSection = document.getElementById("naviSection");
const appsSection = document.getElementById("appsSection");
const statusEl = document.getElementById("status");
const searchEl = document.getElementById("search");
const catEl = document.getElementById("category");

let curated = [];
let activeSource = "";

function load() {
  // catalog.js inlines the catalog as a classic script, so this works from
  // file:// with no fetch(), no local server, and no CORS workaround.
  if (!Array.isArray(window.NAVIAPPS_CATALOG)) {
    statusEl.textContent =
      "couldn't load the catalog — catalog.js is missing; re-run build-catalog.sh.";
    return;
  }
  curated = window.NAVIAPPS_CATALOG;
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

function badgeFor() {
  return '<span class="badge webapp">webapp</span>';
}

function matches(a, q, cat) {
  if (activeSource === "navi" && a.category !== "navi") return false;
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

function cardHTML(a, i) {
  return `<article class="card" style="--d:${Math.min(i, 20) * 35}ms">
    <div class="top">${artFor(a, 32)}
    <h2>${escapeHTML(a.name)}</h2></div>
    <div class="badges">${badgeFor()}</div>
    <p>${escapeHTML(a.summary)}</p>
    <div class="row">
      <button class="btn primary" data-install="${a.id}">install</button>
      <button class="btn" data-details="${a.id}">details</button>
    </div>
  </article>`;
}

function paintGrid(el, apps) {
  el.innerHTML = apps.map(cardHTML).join("");
}

function render() {
  const q = searchEl.value.trim();
  const cat = catEl.value;
  const isNavi = (a) => a.category === "navi";
  const naviApps = curated.filter((a) => isNavi(a) && matches(a, q, cat));
  const rest = curated.filter((a) => !isNavi(a) && matches(a, q, cat));

  // the "navi" filter spotlights first-party apps; "all" shows both sections
  const showNavi = activeSource === "" || activeSource === "navi";
  const showRest = activeSource === "";

  naviSection.hidden = !(showNavi && naviApps.length);
  appsSection.hidden = !(showRest && rest.length);
  if (!naviSection.hidden) paintGrid(naviGrid, naviApps);
  if (!appsSection.hidden) paintGrid(grid, rest);

  const total = (showNavi ? naviApps.length : 0) + (showRest ? rest.length : 0);
  statusEl.textContent = total
    ? `${total} app${total === 1 ? "" : "s"}`
    : "no apps found. try another search.";
  wireButtons();
}

function wireButtons() {
  for (const root of [grid, naviGrid]) {
    root.querySelectorAll("[data-details]").forEach((b) =>
      b.addEventListener("click", () => openModal(findApp(b.dataset.details)))
    );
    root.querySelectorAll("[data-install]").forEach((b) =>
      b.addEventListener("click", () => openModal(findApp(b.dataset.install), true))
    );
  }
}

function findApp(id) {
  return curated.find((a) => a.id === id);
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
  mMeta.textContent = `webapp · ${a.package} · ${a.category}`;
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
  const mRemoveWrap = document.getElementById("mRemoveWrap");
  mRemoveWrap.hidden = false;
  document.getElementById("mRemove").textContent = `navi-webapp --uninstall ${a.package}`;
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

searchEl.addEventListener("input", render);
catEl.addEventListener("change", render);

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
