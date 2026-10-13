"use strict";
(() => {
  const T = {
    en: {
      pair_title: "Pair this phone", pair_intro: "Give this phone a name. The computer will show a code to compare before it allows access.",
      device_name: "Phone name", pair_button: "Pair", pair_compare: "Check that the computer shows the same code, then accept there:",
      pair_waiting: "Waiting for acceptance on the computer…", pair_need_qr: "Open the pairing QR code from Atlas on your computer (/mobile) and scan it with this phone.",
      pair_rejected: "Pairing was declined on the computer.", pair_expired: "The pairing request expired. Show a new QR code on the computer.",
      tasks: "Tasks", no_tasks: "No tasks yet.", pause: "Pause", resume: "Resume", stop: "Stop", preview: "Screen preview",
      message: "Message", attach: "Attach", send: "Send", settings: "Settings", this_device: "This phone", certificate: "Certificate",
      previews: "Screen previews", notifications: "Notify me about questions and permission requests",
      notify_note: "Notifications appear only while this page is open in the browser. Atlas does not use a push service.",
      sign_out: "Unpair this phone", connected: "Connected", reconnecting: "Reconnecting…", offline: "Disconnected",
      st_running: "Running", st_idle: "Idle", st_permission: "Needs permission", st_question: "Question", st_paused: "Paused",
      perm_title: "Permission request", allow: "Allow once", allow_session: "Allow for task", deny: "Deny",
      q_title: "Question", yes: "Yes", no: "No", submit: "Submit", cancel: "Cancel",
      stop_confirm: "Stop this task now? Running steps are canceled.", stale: "Already answered or changed elsewhere. Showing the latest state.",
      failed: "Request failed", sent: "Sent", queued: "Queued after the current run", run_error: "Run failed",
      previews_on: "On (enabled from the computer)", previews_off: "Off — enable for this phone in Atlas on the computer",
      updated: "Updated", todos: "steps", you: "You", atlas: "Atlas", resynced: "Reconnected; state reloaded.",
      notify_denied: "The browser blocked notifications for this page.", revoked: "This phone was unpaired.",
      notify_unsupported: "Atlas cannot send background notifications on this device. Open the panel to see questions and permission requests.",
      iphone_install_title: "Atlas on your iPhone",
      iphone_install_steps: "In Safari, tap Share → Add to Home Screen. Leave Open as Web App enabled if shown, then tap Add.",
      iphone_install_note: "Open Atlas from the new icon. If pairing is requested again, open /mobile on the computer and scan a fresh QR code. Keep the computer awake and connected.",
    },
    tr: {
      pair_title: "Bu telefonu eşleştir", pair_intro: "Bu telefona bir ad ver. Bilgisayar, erişime izin vermeden önce karşılaştırman için bir kod gösterecek.",
      device_name: "Telefon adı", pair_button: "Eşleştir", pair_compare: "Bilgisayarda aynı kodun göründüğünü kontrol et ve orada onayla:",
      pair_waiting: "Bilgisayarda onay bekleniyor…", pair_need_qr: "Bilgisayarındaki Atlas'ta eşleştirme QR kodunu aç (/mobile) ve bu telefonla tara.",
      pair_rejected: "Eşleştirme bilgisayarda reddedildi.", pair_expired: "Eşleştirme isteğinin süresi doldu. Bilgisayarda yeni bir QR kod göster.",
      tasks: "Görevler", no_tasks: "Henüz görev yok.", pause: "Duraklat", resume: "Devam et", stop: "Durdur", preview: "Ekran önizleme",
      message: "Mesaj", attach: "Dosya ekle", send: "Gönder", settings: "Ayarlar", this_device: "Bu telefon", certificate: "Sertifika",
      previews: "Ekran önizleme", notifications: "Sorular ve izin istekleri için bildir",
      notify_note: "Bildirimler yalnızca bu sayfa tarayıcıda açıkken görünür. Atlas bir push servisi kullanmaz.",
      sign_out: "Bu telefonun eşleşmesini kaldır", connected: "Bağlı", reconnecting: "Yeniden bağlanıyor…", offline: "Bağlantı yok",
      st_running: "Çalışıyor", st_idle: "Boşta", st_permission: "İzin bekliyor", st_question: "Soru var", st_paused: "Duraklatıldı",
      perm_title: "İzin isteği", allow: "Bir kez izin ver", allow_session: "Görev için izin ver", deny: "Reddet",
      q_title: "Soru", yes: "Evet", no: "Hayır", submit: "Gönder", cancel: "İptal",
      stop_confirm: "Bu görev şimdi durdurulsun mu? Çalışan adımlar iptal edilir.", stale: "Başka yerde yanıtlanmış veya değişmiş. Güncel durum gösteriliyor.",
      failed: "İstek başarısız", sent: "Gönderildi", queued: "Mevcut çalışmadan sonra sıraya alındı", run_error: "Çalışma başarısız",
      previews_on: "Açık (bilgisayardan etkinleştirildi)", previews_off: "Kapalı — bilgisayardaki Atlas'tan bu telefon için aç",
      updated: "Güncellendi", todos: "adım", you: "Sen", atlas: "Atlas", resynced: "Yeniden bağlandı; durum yenilendi.",
      notify_denied: "Tarayıcı bu sayfa için bildirimleri engelledi.", revoked: "Bu telefonun eşleşmesi kaldırıldı.",
      notify_unsupported: "Atlas bu cihazda arka plan bildirimi gönderemiyor. Soruları ve izin isteklerini görmek için paneli aç.",
      iphone_install_title: "iPhone’unda Atlas",
      iphone_install_steps: "Safari’de Paylaş → Ana Ekrana Ekle’ye dokun. Görünüyorsa Web Uygulaması Olarak Aç seçeneğini açık bırak ve Ekle’ye dokun.",
      iphone_install_note: "Atlas’ı yeni simgesinden aç. Yeniden eşleştirme istenirse bilgisayarda /mobile komutuyla yeni bir QR kodu aç ve tara. Bilgisayar açık ve bağlantıda kalmalı.",
    },
  };
  let lang = (navigator.language || "en").toLowerCase().startsWith("tr") ? "tr" : "en";
  const t = (k) => (T[lang] && T[lang][k]) || T.en[k] || k;
  const $ = (id) => document.getElementById(id);
  const state = { me: null, csrf: "", seq: 0, sessions: new Map(), current: null, detail: null, es: null, upload: null };
  const appleMobile = /iPhone|iPad|iPod/.test(navigator.userAgent) || navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1;
  const standalone = () => navigator.standalone === true || window.matchMedia("(display-mode: standalone)").matches;
  let refreshing = null;
  let resumeTimer = null;
  // Reconnect delays grow from 1 s to 30 s with jitter, so a phone does not
  // hammer a relay whose computer is asleep; any success resets them.
  let retryDelay = 1000;
  function scheduleRetry(fn) {
    clearTimeout(resumeTimer);
    const delay = retryDelay + Math.floor(Math.random() * 500);
    retryDelay = Math.min(retryDelay * 2, 30000);
    resumeTimer = setTimeout(fn, delay);
  }

  function applyI18n() {
    document.documentElement.lang = lang;
    document.querySelectorAll("[data-i18n]").forEach((el) => { el.textContent = t(el.dataset.i18n); });
    $("iphone-install").classList.toggle("hidden", !appleMobile || standalone());
    if (appleMobile || !("Notification" in window)) {
      $("notify").disabled = true;
      $("notify").checked = false;
      $("notify-note").textContent = t("notify_unsupported");
    }
  }

  function el(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  function show(view) {
    ["pair", "list", "task", "settings"].forEach((v) => $("view-" + v).classList.toggle("hidden", v !== view));
    $("back").classList.toggle("hidden", view === "list" || view === "pair");
    $("settings-btn").classList.toggle("hidden", view === "pair");
    if (view === "list") $("title").textContent = "Atlas";
    if (view === "settings") $("title").textContent = t("settings");
    $("main").focus({ preventScroll: true });
  }

  function banner(text, bad) {
    const b = $("banner");
    if (!text) { b.classList.add("hidden"); return; }
    b.textContent = text;
    b.classList.toggle("bad", !!bad);
    b.classList.remove("hidden");
  }

  function setConn(kind) {
    const c = $("conn");
    c.className = "conn " + ({ connected: "ok", reconnecting: "warn", offline: "bad" }[kind] || "");
    c.textContent = t(kind);
  }

  function uuid() {
    if (crypto.randomUUID) return crypto.randomUUID();
    const b = crypto.getRandomValues(new Uint8Array(16));
    return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
  }

  async function api(path, opts = {}) {
    const headers = Object.assign({}, opts.headers || {});
    if (opts.body && !(opts.body instanceof FormData)) headers["Content-Type"] = "application/json";
    if (opts.method && opts.method !== "GET") {
      headers["X-CSRF-Token"] = state.csrf;
      headers["Idempotency-Key"] = opts.key || uuid();
    }
    const resp = await fetch(path, { method: opts.method || "GET", headers, credentials: "same-origin", body: opts.body instanceof FormData ? opts.body : opts.body ? JSON.stringify(opts.body) : undefined });
    let data = null;
    try { data = await resp.json(); } catch (_) { data = null; }
    if (!resp.ok) {
      const err = new Error((data && data.error && data.error.message) || resp.statusText);
      err.status = resp.status;
      err.code = data && data.error && data.error.code;
      throw err;
    }
    return data;
  }

  // Actions retry once on a network failure with the same idempotency key,
  // so a dropped connection never runs the effect twice.
  async function act(path, body, button) {
    const key = uuid();
    if (button) button.disabled = true;
    try {
      try {
        return await api(path, { method: "POST", body, key });
      } catch (e) {
        if (e.status) throw e;
        return await api(path, { method: "POST", body, key });
      }
    } catch (e) {
      if (e.code === "stale_request" || e.code === "stale_revision") {
        banner(t("stale"));
        if (state.current) await loadTask(state.current);
      } else {
        banner(t("failed") + ": " + e.message, true);
      }
      throw e;
    } finally {
      if (button) button.disabled = false;
    }
  }

  function fmtTime(sec) {
    if (!sec) return "";
    const d = new Date(sec * 1000);
    return d.toLocaleTimeString(lang, { hour: "2-digit", minute: "2-digit" });
  }

  function duration(a, b) {
    const s = Math.max(0, (b || Date.now() / 1000) - a);
    if (s < 60) return Math.round(s) + "s";
    if (s < 3600) return Math.round(s / 60) + "m";
    return Math.floor(s / 3600) + "h " + Math.round((s % 3600) / 60) + "m";
  }

  function statusOf(s) {
    if (s.pending_permissions > 0) return ["waiting", t("st_permission")];
    if (s.pending_questions > 0) return ["waiting", t("st_question")];
    if (s.paused) return ["paused", t("st_paused")];
    if (s.busy) return ["running", t("st_running")];
    return ["", t("st_idle")];
  }

  function renderList() {
    const list = $("task-list");
    list.replaceChildren();
    const items = Array.from(state.sessions.values()).sort((a, b) => (b.updated_at || 0) - (a.updated_at || 0));
    $("list-empty").classList.toggle("hidden", items.length > 0);
    for (const s of items) {
      const li = el("li");
      const card = el("button", "card");
      card.type = "button";
      const row = el("div", "row");
      row.append(el("span", "name", s.title || s.id));
      const [cls, label] = statusOf(s);
      row.append(el("span", "badge " + cls, label));
      card.append(row);
      let sub = t("updated") + " " + fmtTime(s.updated_at) + " · " + duration(s.created_at, s.updated_at);
      if (s.todos_total) sub += " · " + s.todos_done + "/" + s.todos_total + " " + t("todos");
      card.append(el("div", "sub", sub));
      card.addEventListener("click", () => openTask(s.id));
      li.append(card);
      list.append(li);
    }
  }

  async function loadList() {
    const data = await api("/api/sessions");
    state.sessions = new Map((data.sessions || []).map((s) => [s.id, s]));
    state.seq = Math.max(state.seq, data.seq || 0);
    renderList();
  }

  function renderMessages(msgs) {
    const ol = $("messages");
    ol.replaceChildren();
    for (const m of msgs) ol.append(messageNode(m));
  }

  function messageNode(m) {
    const li = el("li", "msg " + (m.role === "user" ? "user" : ""));
    li.dataset.id = m.id;
    li.append(el("div", "who", m.role === "user" ? t("you") : t("atlas")));
    if (m.text) li.append(el("div", "text", m.text));
    if (m.tools && m.tools.length) li.append(el("div", "tools", "⚙ " + m.tools.join(", ")));
    return li;
  }

  function upsertMessage(m) {
    const ol = $("messages");
    const prev = ol.querySelector('[data-id="' + CSS.escape(m.id) + '"]');
    const node = messageNode(m);
    if (prev) prev.replaceWith(node); else ol.append(node);
  }

  function renderPending(d) {
    const box = $("pending");
    const drafts = new Map(Array.from(box.querySelectorAll("form[data-request]"), (form) => [form.dataset.request,
      Array.from(form.querySelectorAll("input, textarea"), (input) => ({ type: input.type, value: input.value, checked: input.checked }))]));
    box.replaceChildren();
    for (const p of d.permissions || []) {
      const c = el("div", "pending-card");
      c.append(el("h3", "", t("perm_title") + ": " + p.tool + (p.action ? " · " + p.action : "")));
      if (p.description) c.append(el("p", "", p.description));
      if (p.path) c.append(el("p", "muted small mono", p.path));
      const actions = el("div", "actions");
      for (const [decision, label, cls] of [["allow", t("allow"), "primary"], ["allow_session", t("allow_session"), ""], ["deny", t("deny"), "danger"]]) {
        const b = el("button", "btn " + cls, label);
        b.type = "button";
        b.addEventListener("click", async () => {
          try { await act("/api/permissions/" + encodeURIComponent(p.id), { decision, session_id: p.session_id, tool_call_id: p.tool_call_id }, b); c.remove(); } catch (_) {}
        });
        actions.append(b);
      }
      c.append(actions);
      box.append(c);
    }
    for (const q of d.questions || []) {
      const card = questionCard(q);
      const draft = drafts.get(card.dataset.request);
      if (draft) Array.from(card.querySelectorAll("input, textarea")).forEach((input, index) => {
        const saved = draft[index];
        if (saved && input.type === saved.type) {
          if (input.type === "checkbox" || input.type === "radio") input.checked = saved.checked && input.value === saved.value;
          else input.value = saved.value;
        }
      });
      box.append(card);
    }
    for (const e of d.errors || []) {
      const c = el("div", "pending-card error");
      c.append(el("h3", "", t("run_error")));
      c.append(el("p", "", e));
      box.append(c);
    }
  }

  function questionCard(q) {
    const c = el("form", "pending-card");
    c.dataset.request = JSON.stringify({ id: q.id, session: q.session_id, questions: q.questions });
    c.append(el("h3", "", t("q_title")));
    const inputs = [];
    for (const item of q.questions) {
      c.append(el("p", "", item.question));
      if (item.description) c.append(el("p", "muted small", item.description));
      if (item.type === "yes_no") {
        const row = el("div", "actions");
        for (const [v, label] of [[true, t("yes")], [false, t("no")]]) {
          const lab = el("label", "choice");
          const r = el("input");
          r.type = "radio"; r.name = q.id + item.id; r.value = String(v);
          lab.append(r, el("span", "", label));
          row.append(lab);
        }
        c.append(row);
        inputs.push(() => { const r = c.querySelector('input[name="' + CSS.escape(q.id + item.id) + '"]:checked'); return r ? { question_id: item.id, yes: r.value === "true" } : null; });
      } else if (item.type === "single_choice" || item.type === "multi_choice") {
        for (const ch of item.choices || []) {
          const lab = el("label", "choice");
          const r = el("input");
          r.type = item.type === "single_choice" ? "radio" : "checkbox"; r.name = q.id + item.id; r.value = ch.id;
          lab.append(r, el("span", "", ch.label + (ch.description ? " — " + ch.description : "")));
          c.append(lab);
        }
        inputs.push(() => { const ids = Array.from(c.querySelectorAll('input[name="' + CSS.escape(q.id + item.id) + '"]:checked'), (x) => x.value); return ids.length ? { question_id: item.id, selected_ids: ids } : null; });
      } else {
        const ta = el("textarea");
        ta.rows = 3;
        c.append(ta);
        inputs.push(() => (ta.value.trim() ? { question_id: item.id, fill_in_text: ta.value.trim() } : null));
      }
    }
    const actions = el("div", "actions");
    const submit = el("button", "btn primary", t("submit"));
    submit.type = "submit";
    const cancel = el("button", "btn", t("cancel"));
    cancel.type = "button";
    actions.append(submit, cancel);
    c.append(actions);
    c.addEventListener("submit", async (ev) => {
      ev.preventDefault();
      const answers = inputs.map((f) => f()).filter(Boolean);
      try { await act("/api/questions/" + encodeURIComponent(q.id), { session_id: q.session_id, answers }, submit); c.remove(); } catch (_) {}
    });
    h.append(icon("hand"));
    const tx = el("div");
    tx.append(el("strong", "", t("ctl_title")));
    h.append(tx);
    c.append(h);
    if (d.interaction.reason) {
      const r = el("p", "reason");
      r.append(el("span", "muted", t("ctl_reason") + " "), document.createTextNode(d.interaction.reason));
      c.append(r);
    }
    const b = button("btn primary block", t("ctl_return"));
    b.id = "return-control";
    b.disabled = state.conn !== "connected";
    b.addEventListener("click", () => returnControl(b));
    c.append(b, el("p", "fine", d.interaction.canceled ? t("ctl_canceled_note") : t("ctl_note")));
    return c;
  }
  async function returnControl(b) {
    const d = state.detail;
    if (!d || !d.workflow || !d.workflow.available || state.conn !== "connected") return;
    b.disabled = true;
    b.classList.add("busy");
    try {
      const r = await act("/api/sessions/" + encodeURIComponent(state.current) + "/control", { action: "interaction_resume", revision: d.workflow.revision });
      toast(r && r.canceled ? t("ctl_canceled") : t("ctl_returned"));
    } catch (e) {
      toast(e.code === "not_paused" ? t("ctl_already") : isStale(e) ? t("stale") : t("failed") + ": " + e.message, !(isStale(e) || e.code === "not_paused"));
    } finally {
      b.classList.remove("busy");
      if (state.current) await loadTask(state.current).catch(() => {});
    }
  }

  // ---------- Chat / live screen view ----------
  function applyView(pendingOpen) {
    const screenView = !pendingOpen && state.view === "screen" && screen.open;
    $("scroll").classList.toggle("hidden", pendingOpen || screenView);
    $("composer").classList.toggle("hidden", pendingOpen || screenView);
    $("screen-pane").classList.toggle("hidden", !screenView);
    $("view-switch").classList.toggle("hidden", pendingOpen || !screen.open);
    $("tab-chat").setAttribute("aria-selected", String(!screenView));
    $("tab-screen").setAttribute("aria-selected", String(screenView));
    $("screen-btn").setAttribute("aria-pressed", String(screen.open));
    // The stream runs only while the screen is actually visible.
    if (screenView && !document.hidden) screenConnect();
    else if (screen.ctrl) screenDisconnect(screen.open ? "paused" : "stopped");
  }
  function setView(view) {
    state.view = view;
    applyView(state.pendingOpen && !!state.detail && pendingKeys(state.detail).length > 0);
    if (view === "chat" && nearBottom()) toBottom();
  }

  // ---------- Live screen ----------
  // Frames arrive on one authenticated fetch stream (see screen.go). The
  // phone keeps only the newest undecoded frame, so a slow phone shows the
  // latest picture instead of falling behind.
  const screen = {
    open: false, ctrl: null, conn: "idle", lastSeq: 0, rendered: [], lastRender: 0, lastStats: null,
    zoom: 1, retries: 0, retryTimer: null, staleTimer: null, decoding: false, pending: null, message: "", gaveUp: false,
    metrics: { frames: 0, latency: [], bytes: 0 },
  };
  window.__atlasScreen = screen;
  function screenAllowed() { return !!(state.me && state.me.screen_available && state.me.device && state.me.device.previews); }
  function openScreen() {
    if (!screenAllowed()) { toast(t("scr_off"), true); return; }
    screen.open = true;
    screen.gaveUp = false;
    screen.retries = 0;
    screen.message = "";
    setView("screen");
    renderScreenChrome();
  }
  function closeScreen() {
    screen.open = false;
    exitExpanded();
    screenDisconnect("stopped");
    setView("chat");
    renderScreenChrome();
  }
  function screenDisconnect(conn) {
    clearTimeout(screen.retryTimer);
    screen.retryTimer = null;
    if (screen.ctrl) screen.ctrl.abort();
    screen.ctrl = null;
    screen.pending = null;
    screen.conn = conn || "idle";
    renderScreenChrome();
  }
  async function screenConnect() {
    if (screen.ctrl || !screen.open || screen.gaveUp || document.hidden || !state.me) return;
    clearTimeout(screen.retryTimer);
    const ctrl = new AbortController();
    screen.ctrl = ctrl;
    screen.conn = "connecting";
    // A new connection starts fresh: nothing from an older stream replays.
    screen.lastSeq = 0;
    screen.drawnSeq = 0;
    screen.pending = null;
    renderScreenChrome();
    try {
      const resp = await fetch("/api/screen/stream", { headers: { "X-CSRF-Token": state.csrf }, credentials: "same-origin", cache: "no-store", signal: ctrl.signal });
      if (!resp.ok) {
        let code = "", message = resp.statusText;
        try { const j = await resp.json(); code = j.error.code; message = j.error.message; } catch (_) {}
        if (resp.status === 401) { screen.open = false; screenDisconnect("stopped"); startPairing("expired"); return; }
        if (code === "previews_off") { screen.message = t("scr_off"); screen.gaveUp = true; throw Object.assign(new Error(message), { final: true }); }
        if (resp.status === 429) screen.message = t("scr_busy");
        throw new Error(message);
      }
      const reader = resp.body.getReader();
      let buf = new Uint8Array(0);
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        if (screen.ctrl !== ctrl) return;
        buf = concatBytes(buf, value);
        while (buf.length >= 32) {
          const dv = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);
          if (buf[0] !== 65 || buf[1] !== 84 || buf[2] !== 83 || buf[3] !== 49) throw new Error("bad stream");
          const len = dv.getUint32(28);
          if (len > 4 << 20) throw new Error("frame too large");
          if (buf.length < 32 + len) break;
          const kind = buf[4];
          const seq = Number(dv.getBigUint64(8));
          const captured = Number(dv.getBigInt64(16)) / 1000;
          const w = dv.getUint16(24), h = dv.getUint16(26);
          const payload = buf.slice(32, 32 + len);
          buf = buf.slice(32 + len);
          screen.metrics.bytes += 32 + len;
          if (kind === 1) onScreenFrame(seq, captured, w, h, payload);
          else if (kind === 2) onScreenStatus(payload);
        }
      }
      throw new Error("stream ended");
    } catch (e) {
      if (screen.ctrl !== ctrl) return;
      screen.ctrl = null;
      if (ctrl.signal.aborted) return;
      if (e.final) { screen.conn = "error"; renderScreenChrome(); return; }
      scheduleScreenRetry();
    }
  }
  function concatBytes(a, b) {
    if (!a.length) return b;
    const out = new Uint8Array(a.length + b.length);
    out.set(a, 0);
    out.set(b, a.length);
    return out;
  }
  function scheduleScreenRetry() {
    if (!screen.open) return;
    if (screen.retries >= 8) { screen.gaveUp = true; screen.conn = "error"; screen.message = screen.message || t("scr_gave_up"); renderScreenChrome(); return; }
    const delay = Math.min(15000, 800 * 2 ** screen.retries) + Math.floor(Math.random() * 400);
    screen.retries++;
    screen.conn = "retrying";
    screen.retryAt = Date.now() + delay;
    renderScreenChrome();
    screen.retryTimer = setTimeout(() => { screen.retryTimer = null; screenConnect(); }, delay);
  }
  function onScreenStatus(payload) {
    let st = null;
    try { st = JSON.parse(new TextDecoder().decode(payload)); } catch (_) { return; }
    screen.lastStats = st;
    if (st.state === "revoked") { screen.open = false; screenDisconnect("stopped"); startPairing("unpaired"); return; }
    if (st.state === "previews_off") { screen.gaveUp = true; screen.message = t("scr_off"); screenDisconnect("error"); return; }
    if (st.state === "blank") screen.message = t("scr_blank");
    else if (st.state === "capture_error") screen.message = t("scr_capture_error", st.message || "");
    else screen.message = "";
    if (st.quality_change) $("screen-note").textContent = t("scr_quality", st.quality_change);
    renderScreenChrome();
  }
  function onScreenFrame(seq, captured, w, h, payload) {
    // Late or replayed frames never paint over a newer picture.
    if (seq <= screen.lastSeq) return;
    screen.lastSeq = seq;
    screen.retries = 0;
    screen.conn = "live";
    screen.pending = { seq, captured, w, h, payload };
    if (!screen.decoding) decodeNext();
  }
  async function decodeNext() {
    const f = screen.pending;
    if (!f) return;
    screen.pending = null;
    screen.decoding = true;
    try {
      const blob = new Blob([f.payload], { type: "image/jpeg" });
      let img;
      if (window.createImageBitmap) img = await createImageBitmap(blob);
      else img = await new Promise((res, rej) => { const i = new Image(); const u = URL.createObjectURL(blob); i.onload = () => { URL.revokeObjectURL(u); res(i); }; i.onerror = () => { URL.revokeObjectURL(u); rej(new Error("decode")); }; i.src = u; });
      if (screen.open && f.seq > (screen.drawnSeq || 0)) drawScreen(img, f);
      if (img.close) img.close();
    } catch (_) {
      // A corrupt frame is skipped; the next one replaces it.
    } finally {
      screen.decoding = false;
      if (screen.pending) decodeNext();
    }
  }
  function drawScreen(img, f) {
    const canvas = $("screen-canvas");
    const vp = $("screen-viewport");
    if (canvas.width !== f.w || canvas.height !== f.h) {
      // Keep the reader's relative position when the resolution changes.
      const rx = vp.scrollWidth ? vp.scrollLeft / vp.scrollWidth : 0, ry = vp.scrollHeight ? vp.scrollTop / vp.scrollHeight : 0;
      canvas.width = f.w;
      canvas.height = f.h;
      fitScreen();
      vp.scrollLeft = rx * vp.scrollWidth;
      vp.scrollTop = ry * vp.scrollHeight;
    }
    canvas.getContext("2d", { alpha: false }).drawImage(img, 0, 0, f.w, f.h);
    screen.drawnSeq = f.seq;
    const now = Date.now();
    screen.lastRender = now;
    screen.lastCapture = f.captured;
    screen.rendered.push(now);
    while (screen.rendered.length && now - screen.rendered[0] > 2000) screen.rendered.shift();
    screen.metrics.frames++;
    // Capture-to-render age by the two clocks; meaningful only when the
    // phone and computer clocks agree (tests run both on one machine).
    screen.metrics.latency.push(now - f.captured);
    if (screen.metrics.latency.length > 600) screen.metrics.latency.shift();
    if (!screen.chromeQueued) { screen.chromeQueued = true; requestAnimationFrame(() => { screen.chromeQueued = false; renderScreenChrome(); }); }
  }
  function measuredFPS() {
    const r = screen.rendered;
    if (r.length < 2) return 0;
    const span = (r[r.length - 1] - r[0]) / 1000;
    return span > 0 ? (r.length - 1) / span : 0;
  }
  function fitScreen() {
    const pane = $("screen-pane"), vp = $("screen-viewport"), canvas = $("screen-canvas");
    if (!pane.clientWidth || !pane.clientHeight) return;
    const style = getComputedStyle(pane);
    const children = [...pane.children].filter(el => {
      const s = getComputedStyle(el);
      return s.display !== "none" && s.position !== "absolute";
    });
    const otherHeight = children.filter(el => el !== vp).reduce((sum, el) => sum + el.getBoundingClientRect().height, 0);
    const width = Math.max(1, pane.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight) - 2);
    const height = Math.max(1, pane.clientHeight - parseFloat(style.paddingTop) - parseFloat(style.paddingBottom) - otherHeight - parseFloat(style.rowGap) * (children.length - 1) - 2);
    // Fit the whole frame, then zoom inside this fixed, scrollable viewport.
    const scale = Math.min(width / canvas.width, height / canvas.height);
    vp.style.width = (canvas.width * scale + 2) + "px";
    vp.style.height = (canvas.height * scale + 2) + "px";
  }
  function renderScreenChrome() {
    if (!$("screen-pane")) return;
    fitScreen();
    const now = Date.now();
    const age = screen.lastRender ? (now - screen.lastRender) / 1000 : Infinity;
    const live = screen.conn === "live" && age < 2;
    const stale = screen.open && !live && screen.lastRender > 0;
    const label = { connecting: "scr_connecting", retrying: "scr_connecting", live: live ? "scr_live" : "scr_stale", paused: "scr_paused", stopped: "scr_stopped", error: "scr_error", idle: "scr_stopped" }[screen.conn] || "scr_stopped";
    $("screen-state").textContent = t(label);
    $("screen-dot").className = "dot " + (live ? "ok" : screen.conn === "error" ? "bad" : screen.conn === "connecting" || screen.conn === "retrying" || stale ? "warn" : "idle");
    const parts = [];
    if (live) parts.push(t("scr_fps", measuredFPS().toFixed(1)));
    if (screen.lastRender) parts.push(t("scr_age", Math.max(0, age).toFixed(age < 10 ? 1 : 0)));
    const canvas = $("screen-canvas");
    if (screen.lastRender) parts.push(t("scr_res", canvas.width, canvas.height));
    $("screen-metrics").textContent = parts.join(" · ");
    const st = screen.lastStats;
    if (st && live && st.capture_fps !== undefined) $("screen-note").textContent = t("scr_measured", Number(st.capture_fps).toFixed(0), Number(st.sent_fps || 0).toFixed(0), Math.round((st.bytes_per_sec || 0) / 1024));
    else if (!screen.lastRender) $("screen-note").textContent = t("scr_only_view");
    const vp = $("screen-viewport");
    vp.classList.toggle("stale", stale);
    let overlay = "";
    if (screen.message) overlay = screen.message;
    else if (screen.conn === "retrying") overlay = t("scr_retry_wait", Math.max(0, Math.ceil(((screen.retryAt || now) - now) / 1000)));
    else if (stale && screen.conn !== "paused") overlay = t("scr_stale_text", Math.round(age));
    else if (screen.conn === "error") overlay = t("scr_gave_up");
    $("screen-overlay").classList.toggle("hidden", !overlay || (live && !screen.message));
    $("screen-overlay-text").textContent = overlay;
    $("screen-retry").classList.toggle("hidden", !(screen.gaveUp || screen.conn === "error"));
    $("screen-zoom").textContent = Math.round(screen.zoom * 100) + "%";
    const exp = $("screen-pane").classList.contains("expanded") || document.fullscreenElement === $("screen-pane");
    $("screen-full").setAttribute("aria-label", t(exp ? "exit_fullscreen" : "fullscreen"));
    $("screen-full").title = t(exp ? "exit_fullscreen" : "fullscreen");
  }
  function startScreenClock() {
    clearInterval(screen.staleTimer);
    screen.staleTimer = setInterval(() => { if (screen.open && !document.hidden) renderScreenChrome(); }, 500);
  }
  function setZoom(z, cx, cy) {
    const vp = $("screen-viewport");
    const old = screen.zoom;
    z = Math.min(4, Math.max(1, z));
    if (z === old) return;
    // Zoom around a point (center by default) so the view does not jump.
    const px = cx === undefined ? vp.clientWidth / 2 : cx, py = cy === undefined ? vp.clientHeight / 2 : cy;
    const fx = (vp.scrollLeft + px) / vp.scrollWidth, fy = (vp.scrollTop + py) / vp.scrollHeight;
    screen.zoom = z;
    vp.style.setProperty("--zoom", String(z));
    vp.classList.toggle("zoomed", z > 1);
    vp.scrollLeft = fx * vp.scrollWidth - px;
    vp.scrollTop = fy * vp.scrollHeight - py;
    renderScreenChrome();
  }
  function exitExpanded() {
    $("screen-pane").classList.remove("expanded");
    if (document.fullscreenElement && document.exitFullscreen) document.exitFullscreen().catch(() => {});
  }
  function toggleFull() {
    const pane = $("screen-pane");
    if (document.fullscreenElement === pane || pane.classList.contains("expanded")) { exitExpanded(); renderScreenChrome(); return; }
    // iPhone Safari has no element full screen; a fixed overlay stands in.
    if (pane.requestFullscreen && !appleMobile) pane.requestFullscreen().catch(() => pane.classList.add("expanded")).finally(renderScreenChrome);
    else { pane.classList.add("expanded"); renderScreenChrome(); }
  }

  // ---------- Settings ----------
  function renderSettings() {
    const me = state.me;
    if (!me) return;
    $("set-device").textContent = me.device.name;
    $("set-paired").textContent = me.device.paired_at ? t("paired_on", new Date(me.device.paired_at).toLocaleDateString(locale(), { day: "numeric", month: "long", year: "numeric" })) : "";
    $("set-computer").textContent = computerName() + (me.computer && me.computer.os ? " · " + me.computer.os : "");
    $("set-fp").textContent = me.fingerprint || "";
    $("set-cert-note").textContent = t(me.tailscale ? "tailscale_note" : "certificate_note");
    $("set-version").textContent = me.version ? t("version", me.version) : "Atlas Remote";
    $("set-previews").textContent = !me.preview_available ? t("previews_unavailable") : me.device.previews ? t("previews_on") : t("previews_off");
    $("set-previews-note").textContent = !me.preview_available ? "" : me.device.previews ? t("previews_on_note") : t("previews_off_note");
    // In-app alerts remain available independently of browser permission.
    const n = $("notify");
    n.disabled = false;
    n.checked = store.get("atlas.notify") === "1";
    $("notify-note").textContent = t("notify_note");
    const installed = standalone();
    $("iphone-install").classList.toggle("hidden", !appleMobile || installed || inAppBrowser);
    $("android-install").classList.toggle("hidden", appleMobile || installed);
    $("install-details").classList.toggle("hidden", installed || inAppBrowser);
    $("install-section").classList.toggle("hidden", !installed && !appleMobile && !android);
    // Only a real beforeinstallprompt enables the button; otherwise the
    // browser menu is the way, and the text says so.
    $("install-prompt-wrap").classList.toggle("hidden", installed || !state.installPrompt);
    $("transfer-wrap").classList.toggle("hidden", installed || !appleMobile || inAppBrowser);
    $("install-summary").textContent = installed ? t("installed") : inAppBrowser ? t("install_inapp") : appleMobile ? t("install_ios_summary") : state.installPrompt ? "" : t("install_android_summary") + " " + t("install_android_none");
    applyTheme();
  }

  // ---------- Events ----------
  function notify(title, body) {
    try {
      if (store.get("atlas.notify") !== "1") return;
      if (!document.hidden) { toast(title + (body ? ": " + body : "")); return; }
      if ("Notification" in window && Notification.permission === "granted") new Notification(title, { body, tag: "atlas" });
    } catch (_) {}
  }
  let listFrame = 0;
  function onEvent(e) {
    // The stream resumes from the last seen sequence number; anything at or
    // below it was already applied, so replays never duplicate.
    if (e.seq && e.seq <= state.seq) return;
    state.seq = Math.max(state.seq, e.seq || 0);
    state.lastSync = Date.now();
    const s = state.sessions.get(e.session_id);
    switch (e.type) {
      case "session":
        state.sessions.set(e.session_id, Object.assign(s || {}, e.data));
        break;
      case "busy":
        if (s) s.busy = e.data.busy;
        break;
      case "permission":
        if (s) s.pending_permissions = (s.pending_permissions || 0) + 1;
        notify(t("announce_permission"), e.data.tool + ": " + (e.data.description || ""));
        if (state.current === e.session_id) announce(t("announce_permission"));
        break;
      case "permission_resolved":
        if (s) s.pending_permissions = Math.max(0, (s.pending_permissions || 1) - 1);
        break;
      case "question":
        if (s) s.pending_questions = (s.pending_questions || 0) + 1;
        notify(t("announce_question"), (e.data.questions && e.data.questions[0] && e.data.questions[0].question) || "");
        if (state.current === e.session_id) announce(t("announce_question"));
        break;
      case "question_resolved":
        if (s) s.pending_questions = Math.max(0, (s.pending_questions || 1) - 1);
        break;
      case "credential":
        if (s) s.pending_credentials = (s.pending_credentials || 0) + 1;
        notify(t("announce_credential"), (e.data && (e.data.service || e.data.origin)) || "");
        if (state.current === e.session_id) announce(t("announce_credential"));
        break;
      case "credential_resolved":
        if (s) s.pending_credentials = Math.max(0, (s.pending_credentials || 1) - 1);
        break;
      case "run_error": {
        const list = state.errors.get(e.session_id) || [];
        list.push(e.data.message);
        state.errors.set(e.session_id, list.slice(-3));
        break;
      }
    }
    if (!$("view-list").classList.contains("hidden")) {
      cancelAnimationFrame(listFrame);
      listFrame = requestAnimationFrame(renderList);
    }
    if (state.current === e.session_id && state.detail) {
      const d = state.detail;
      if (e.type === "message") {
        const msgs = d.messages || (d.messages = []);
        const i = msgs.findIndex((m) => m.id === e.data.id);
        if (i >= 0) msgs[i] = e.data; else msgs.push(e.data);
        if (msgs.length > 200) msgs.splice(0, msgs.length - 200);
        renderMessages(false);
        renderTaskChrome();
      } else if (e.type === "busy") {
        d.session.busy = e.data.busy;
        renderTaskChrome();
        if (!e.data.busy) scheduleReload();
      } else if (e.type === "run_error") {
        renderRunErrors();
      } else if (["permission", "permission_resolved", "question", "question_resolved", "credential", "credential_resolved", "control", "session"].includes(e.type)) {
        scheduleReload();
      }
    }
  }

  function connect() {
    if (state.es) state.es.close();
    state.es = null;
    if (!state.me || document.hidden) return;
    if (!navigator.onLine) { setConn("offline", "network"); return; }
    if (state.conn !== "connected") setConn("reconnecting");
    const es = new EventSource("/api/events?since=" + state.seq);
    state.es = es;
    es.addEventListener("open", () => { retryDelay = 1000; state.retries = 0; setConn("connected"); });
    es.addEventListener("update", (m) => { try { onEvent(JSON.parse(m.data)); } catch (_) {} });
    es.addEventListener("resync", async (m) => {
      try { state.seq = JSON.parse(m.data).seq || state.seq; } catch (_) {}
      await loadList().catch(() => {});
      if (state.current && !$("view-task").classList.contains("hidden")) await loadTask(state.current).catch(() => {});
    });
    es.addEventListener("revoked", () => { es.close(); startPairing("unpaired"); });
    es.addEventListener("error", () => {
      if (state.es !== es) return;
      if (!navigator.onLine) setConn("offline", "network");
      else if (state.conn === "connected") setConn("reconnecting");
      // The browser retries a dropped stream itself, but gives up after an
      // HTTP error such as 503 while the computer is offline.
      if (es.readyState === 2) {
        setConn("offline", navigator.onLine ? "computer" : "network");
        scheduleRetry(resumeConnection);
      }
    });
  }

  // iOS suspends web apps when locked or backgrounded. Refresh authentication
  // and snapshots before opening a new stream; never replay a user action.
  function resumeConnection() {
    if (document.hidden || !state.me) return Promise.resolve();
    if (!navigator.onLine) { setConn("offline", "network"); return Promise.resolve(); }
    if (refreshing) return refreshing;
    refreshing = (async () => {
      if (state.es) state.es.close();
      state.es = null;
      if (state.conn !== "connected") setConn("reconnecting");
      try {
        const me = await api("/api/me");
        if (!state.me) return;
        state.me = me;
        state.csrf = me.csrf;
        state.seq = Math.max(state.seq, me.seq || 0);
        const active = state.current;
        await loadList();
        if (active && state.current === active && !$("view-task").classList.contains("hidden")) {
          state.paneKey = "";
          await loadTask(active);
        }
        connect();
      } catch (e) {
        if (e.status === 401) startPairing("expired");
        else {
          setConn("offline", !navigator.onLine ? "network" : "computer");
          scheduleRetry(resumeConnection);
        }
      }
    })().finally(() => { refreshing = null; });
    return refreshing;
  }

  async function boot() {
    try {
      const me = await api("/api/me");
      state.me = me;
      state.csrf = me.csrf;
      if (me.language && T[me.language.slice(0, 2)]) lang = me.language.slice(0, 2);
      applyI18n();
      state.seq = me.seq || 0;
      state.retries = 0;
      history.replaceState({ view: "list" }, "", "/");
      show("list");
      setConn("reconnecting");
      await loadList().catch(() => {});
      connect();
    } catch (e) {
      if (e.status === 401) startPairing();
      else {
        state.listStatus = "error";
        setConn("offline", navigator.onLine ? "computer" : "network");
        show("list");
        scheduleRetry(boot);
      }
    }
  }

  // ---------- Pairing ----------
  function pairStage(stage) {
    for (const s of ["none", "invalid", "start", "wait", "done", "transfer"]) $("pair-" + s).classList.toggle("hidden", s !== stage && !(stage === "invalid" && s === "none"));
  }
  function startPairing(reason) {
    if (state.es) state.es.close();
    state.es = null;
    state.me = null;
    state.csrf = "";
    leaveTask();
    state.upload = null;
    state.seq = 0;
    state.sessions = new Map();
    state.listStatus = "loading";
    clearTimeout(resumeTimer);
    stopPairTimers();
    applyI18n();
    show("pair");
    state.conn = "offline";
    const nonce = pairNonce;
    $("pair-error").classList.add("hidden");
    $("transfer-error").classList.add("hidden");
    if (reason === "unpaired" || reason === "expired") {
      $("pair-invalid").querySelector("strong").textContent = t(reason + "_title");
      $("pair-invalid-text").textContent = t(reason);
      pairStage("invalid");
      // A Home Screen app cannot open a scanned QR link itself.
      if (standalone()) $("pair-transfer").classList.remove("hidden");
    } else if (nonce) {
      pairStage("start");
    } else if (standalone()) {
      // iOS gives Home Screen apps their own cookie store; scanning a QR
      // code would open Safari, not this app. A code from the paired
      // browser connects it instead of looping on the pairing screen.
      pairStage("transfer");
    } else {
      pairStage("none");
    }
    $("pair-name").value = store.get("atlas.name") || "";
    $("pair-start").onsubmit = (ev) => { ev.preventDefault(); requestPairing(nonce); };
  }
  async function requestPairing(nonce) {
    const name = $("pair-name").value.trim() || (appleMobile ? "iPhone" : android ? "Android" : "Phone");
    store.set("atlas.name", name);
    const go = $("pair-go");
    go.disabled = true;
    go.classList.add("busy");
    $("pair-error").classList.add("hidden");
    try {
      const resp = await fetch("/api/pair", { method: "POST", headers: { "Content-Type": "application/json" }, credentials: "same-origin", body: JSON.stringify({ nonce, name }) });
      let data = {};
      try { data = await resp.json(); } catch (_) {}
      if (!resp.ok) {
        const code = data.error && data.error.code;
        if (code === "pairing_invalid" || resp.status === 403) {
          history.replaceState(null, "", "/");
          pairNonce = null;
          $("pair-invalid").querySelector("strong").textContent = t("pair_invalid_title");
          $("pair-invalid-text").textContent = t("pair_invalid");
          pairStage("invalid");
          return;
        }
        throw new Error((data.error && data.error.message) || resp.statusText);
      }
      history.replaceState(null, "", "/");
      pairNonce = null;
      state.pair = { id: data.pending_id, poll: data.poll, expires: data.expires || Date.now() + 300000, cancelled: false };
      $("pair-code").textContent = data.code.slice(0, 3) + " " + data.code.slice(3);
      $("pair-code").setAttribute("aria-label", data.code.split("").join(" "));
      $("pair-os").textContent = data.computer_os || "";
      $("pair-sent-name").value = data.name || name;
      pairStage("wait");
      pairCountdown();
      pollPairing(state.pair);
    } catch (e) {
      $("pair-error").textContent = e instanceof TypeError ? t("pair_network") : t("failed") + ": " + e.message;
      $("pair-error").classList.remove("hidden");
    } finally {
      go.disabled = false;
      go.classList.remove("busy");
    }
  }
  let pairTick = null;
  let transferTimer = null;
  function stopPairTimers() { clearInterval(pairTick); pairTick = null; if (state.pair) state.pair.stopped = true; }
  function pairCountdown() {
    const update = () => {
      const p = state.pair;
      if (!p) return;
      const left = Math.max(0, Math.round((p.expires - Date.now()) / 1000));
      $("pair-status").textContent = t("pair_waiting_left", Math.floor(left / 60) + ":" + String(left % 60).padStart(2, "0"));
      if (left <= 0) { pairDone("expired"); }
    };
    clearInterval(pairTick);
    update();
    pairTick = setInterval(update, 1000);
  }
  function pairDone(kind, unsure) {
    stopPairTimers();
    const box = $("pair-done-box");
    box.className = "notice " + (kind === "rejected" || kind === "expired" ? "warn" : "");
    $("pair-done-title").textContent = t("pair_" + kind + "_title");
    $("pair-done-text").textContent = unsure ? t("pair_cancel_unsure") : t("pair_" + kind);
    $("pair-again").classList.remove("hidden");
    pairStage("done");
  }
  async function pairRequest(path, p) {
    const resp = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, credentials: "same-origin", body: JSON.stringify({ pending_id: p.id, poll: p.poll }) });
    let data = {};
    try { data = await resp.json(); } catch (_) {}
    return { resp, data };
  }
  async function pollPairing(p) {
    let delay = 1500;
    while (!p.stopped) {
      await new Promise((r) => setTimeout(r, delay));
      if (p.stopped) return;
      try {
        const { resp, data } = await pairRequest("/api/pair/poll", p);
        if (p.stopped) return;
        if (resp.status === 404) { pairDone("expired"); return; }
        if (!resp.ok) throw new Error(resp.statusText);
        delay = 1500;
        if (data.state === "accepted") { stopPairTimers(); state.pair = null; boot(); return; }
        if (data.state === "rejected" || data.state === "expired" || data.state === "cancelled") { pairDone(data.state); return; }
      } catch (_) {
        // A dropped network is not a decision; keep waiting until expiry.
        delay = Math.min(delay * 2, 10000);
      }
    }
  }
  async function cancelPairing() {
    const p = state.pair;
    if (!p) return;
    p.stopped = true;
    clearInterval(pairTick);
    const b = $("pair-cancel");
    b.disabled = true;
    try {
      const { resp, data } = await pairRequest("/api/pair/cancel", p);
      if (resp.ok && data.state === "cancelled") { pairDone("cancelled"); return; }
      // Already decided: report what really happened.
      const poll = await pairRequest("/api/pair/poll", p);
      if (poll.resp.ok && poll.data.state === "accepted") { state.pair = null; boot(); return; }
      pairDone(poll.resp.ok && poll.data.state === "rejected" ? "rejected" : "expired");
    } catch (_) {
      pairDone("cancelled", true);
    } finally {
      b.disabled = false;
    }
  }

  // ---------- Wiring ----------
  $("pair-help-btn").addEventListener("click", (e) => {
    const open = $("pair-help").classList.toggle("hidden") === false;
    e.currentTarget.setAttribute("aria-expanded", String(open));
  });
  $("pair-cancel").addEventListener("click", cancelPairing);
  $("pair-transfer").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const go = $("transfer-go");
    const code = $("transfer-code").value.trim();
    if (!code || go.disabled) return;
    go.disabled = true;
    go.classList.add("busy");
    $("transfer-error").classList.add("hidden");
    try {
      const resp = await fetch("/api/transfer/redeem", { method: "POST", headers: { "Content-Type": "application/json" }, credentials: "same-origin", cache: "no-store", body: JSON.stringify({ code }) });
      if (!resp.ok) throw Object.assign(new Error(t("transfer_invalid")), { status: resp.status });
      $("transfer-code").value = "";
      boot();
    } catch (e) {
      $("transfer-error").textContent = e.status ? t("transfer_invalid") : t("pair_network");
      $("transfer-error").classList.remove("hidden");
    } finally {
      go.disabled = false;
      go.classList.remove("busy");
    }
  });
  $("transfer-btn").addEventListener("click", async (e) => {
    const b = e.currentTarget;
    b.disabled = true;
    try {
      const data = await act("/api/transfer", {});
      const out = $("transfer-code-out");
      out.textContent = data.code;
      out.classList.remove("hidden");
      const note = $("transfer-code-note");
      note.classList.remove("hidden");
      const until = data.expires;
      clearInterval(transferTimer);
      const upd = () => {
        const left = until - Date.now();
        if (left <= 0) { out.classList.add("hidden"); note.classList.add("hidden"); clearInterval(transferTimer); return; }
        note.textContent = t("transfer_code_note") + " " + fmtLeft(left);
      };
      upd();
      transferTimer = setInterval(upd, 1000);
    } catch (err) {
      toast(t("failed") + ": " + err.message, true);
    } finally {
      b.disabled = false;
    }
  });
  $("install-btn").addEventListener("click", async () => {
    const p = state.installPrompt;
    if (!p) return;
    state.installPrompt = null;
    try {
      await p.prompt();
      await p.userChoice;
    } catch (_) {}
    // Success is reported by the appinstalled event, not by this tap.
    renderSettings();
  });
  window.addEventListener("beforeinstallprompt", (e) => {
    e.preventDefault();
    state.installPrompt = e;
    if (!$("view-settings").classList.contains("hidden")) renderSettings();
  });
  window.addEventListener("appinstalled", () => {
    state.installPrompt = null;
    toast(t("install_done"));
    if (!$("view-settings").classList.contains("hidden")) renderSettings();
  });
  $("update-reload").addEventListener("click", () => location.reload());
  $("pair-again").addEventListener("click", () => { $("pair-again").classList.add("hidden"); pairStage("none"); });
  $("settings-btn").addEventListener("click", () => navigate("settings"));
  $("host-row").addEventListener("click", () => navigate("settings"));
  $("settings-back").addEventListener("click", goBack);
  $("back").addEventListener("click", goBack);
  $("search").addEventListener("input", (e) => { state.query = e.target.value; renderList(); });
  $("waiting-banner").addEventListener("click", () => { state.pendingOpen = true; state.paneKey = ""; renderTaskChrome(); const h = $("pending-h"); if (h) h.focus(); });
  $("new-msgs").addEventListener("click", toBottom);
  $("scroll").addEventListener("scroll", () => { if (nearBottom()) { state.unseen = false; $("new-msgs").classList.add("hidden"); } }, { passive: true });

  function closeMenu() { $("task-menu").classList.add("hidden"); $("menu-btn").setAttribute("aria-expanded", "false"); }
  $("menu-btn").addEventListener("click", (e) => {
    e.stopPropagation();
    const open = $("task-menu").classList.toggle("hidden") === false;
    $("menu-btn").setAttribute("aria-expanded", String(open));
    if (open) { const first = $("task-menu").querySelector("button:not(.hidden):not(:disabled)"); if (first) first.focus(); }
  });
  document.addEventListener("click", (e) => { if (!$("task-menu").contains(e.target)) closeMenu(); });
  document.addEventListener("keydown", (e) => { if (e.key === "Escape" && !$("task-menu").classList.contains("hidden")) { closeMenu(); $("menu-btn").focus(); } });
  $("m-pause").addEventListener("click", (e) => { closeMenu(); control("pause", e.currentTarget); });
  $("m-resume").addEventListener("click", (e) => { closeMenu(); control("resume", e.currentTarget); });
  $("m-refresh").addEventListener("click", () => { closeMenu(); manualRetry(); });
  $("m-screen").addEventListener("click", () => { closeMenu(); openScreen(); });
  $("screen-btn").addEventListener("click", () => { if (screen.open && state.view === "screen") closeScreen(); else openScreen(); });
  $("tab-chat").addEventListener("click", () => setView("chat"));
  $("tab-screen").addEventListener("click", () => { if (screen.open) setView("screen"); });
  $("screen-stop").addEventListener("click", closeScreen);
  $("screen-retry").addEventListener("click", () => { screen.gaveUp = false; screen.retries = 0; screen.message = ""; screenDisconnect("idle"); applyView(false); });
  $("screen-zoom-in").addEventListener("click", () => setZoom(screen.zoom * 1.5));
  $("screen-zoom-out").addEventListener("click", () => setZoom(screen.zoom / 1.5));
  $("screen-fit").addEventListener("click", () => setZoom(1));
  $("screen-full").addEventListener("click", toggleFull);
  $("screen-viewport").addEventListener("dblclick", (e) => {
    const r = e.currentTarget.getBoundingClientRect();
    setZoom(screen.zoom > 1 ? 1 : 2, e.clientX - r.left, e.clientY - r.top);
  });
  document.addEventListener("fullscreenchange", renderScreenChrome);
  if (window.ResizeObserver) {
    const observer = new ResizeObserver(fitScreen);
    observer.observe($("screen-pane"));
    observer.observe($("screen-pane").querySelector(".screen-tools"));
  }
  document.addEventListener("keydown", (e) => { if (e.key === "Escape" && $("screen-pane").classList.contains("expanded")) { exitExpanded(); renderScreenChrome(); } });
  startScreenClock();
  $("stop-btn").addEventListener("click", (e) => control("stop", e.currentTarget));
  $("m-preview").addEventListener("click", async () => {
    closeMenu();
    try {
      const resp = await fetch("/api/preview", { credentials: "same-origin", cache: "no-store" });
      if (!resp.ok) { let m = resp.statusText; try { m = (await resp.json()).error.message; } catch (_) {} throw new Error(m); }
      const img = $("preview-img");
      if (img.src) URL.revokeObjectURL(img.src);
      img.src = URL.createObjectURL(await resp.blob());
      img.alt = t("preview_alt", fmtClock(Date.now() / 1000));
      img.classList.remove("hidden");
      scroller().scrollTop = 0;
    } catch (e) { toast(t("failed") + ": " + e.message, true); }
  });

  $("prompt").addEventListener("input", () => { autoGrow(); saveDraft(); updateComposer(); });
  $("prompt").addEventListener("keydown", (e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) { e.preventDefault(); sendPrompt(); } });
  $("prompt").addEventListener("focus", () => { if (nearBottom()) setTimeout(toBottom, 250); });
  $("composer").addEventListener("submit", (e) => { e.preventDefault(); sendPrompt(); });
  $("attach").addEventListener("change", async (e) => {
    const f = e.target.files[0];
    e.target.value = "";
    if (!f || !state.current || state.conn !== "connected") return;
    const up = { name: f.name, size: f.size, state: "uploading" };
    state.upload = up;
    renderAttachment();
    updateComposer();
    const fd = new FormData();
    fd.append("file", f);
    try {
      const data = await act("/api/sessions/" + encodeURIComponent(state.current) + "/attachments", fd);
      if (state.upload !== up) return;
      Object.assign(up, { id: data.attachment_id, name: data.name, size: data.size, state: "ready" });
    } catch (err) {
      if (state.upload !== up) return;
      Object.assign(up, { state: "failed", error: err.message });
    }
    renderAttachment();
    updateComposer();
  });
  // Removing an unsent attachment drops it from this message; the computer
  // discards unused uploads after 30 minutes.
  $("attach-remove").addEventListener("click", () => { state.upload = null; renderAttachment(); updateComposer(); });

  document.querySelectorAll('input[name="theme"]').forEach((r) => r.addEventListener("change", () => {
    store.set("atlas.theme", r.value === "system" ? null : r.value);
    applyTheme();
  }));
  $("notify").addEventListener("change", async (e) => {
    const box = e.target;
    store.set("atlas.notify", box.checked ? "1" : "0");
    if (box.checked && !appleMobile && "Notification" in window && Notification.permission === "default") {
      try { await Notification.requestPermission(); } catch (_) {}
    }
    renderSettings();
  });
  $("logout").addEventListener("click", () => {
    const open = $("logout-confirm").classList.toggle("hidden") === false;
    $("logout").setAttribute("aria-expanded", String(open));
    $("logout-error").classList.add("hidden");
    if (open) $("logout-no").focus();
  });
  $("logout-no").addEventListener("click", () => { $("logout-confirm").classList.add("hidden"); $("logout").setAttribute("aria-expanded", "false"); $("logout").focus(); });
  $("logout-yes").addEventListener("click", async (e) => {
    const b = e.currentTarget;
    b.disabled = true;
    b.classList.add("busy");
    try {
      await act("/api/logout", {});
      $("logout-confirm").classList.add("hidden");
      startPairing("unpaired");
    } catch (err) {
      if (err.status !== 401) {
        $("logout-error").textContent = t("failed") + ": " + err.message;
        $("logout-error").classList.remove("hidden");
      }
    } finally {
      b.disabled = false;
      b.classList.remove("busy");
    }
  });

  // The chat fills the visual viewport, so the on-screen keyboard never
  // covers the last line or the send button.
  function fitViewport() {
    const vv = window.visualViewport;
    const h = vv ? vv.height : window.innerHeight;
    document.documentElement.style.setProperty("--app-h", Math.round(h) + "px");
    const c = $("composer");
    if (c) document.documentElement.style.setProperty("--composer-h", c.offsetHeight + "px");
  }
  if (window.visualViewport) {
    window.visualViewport.addEventListener("resize", fitViewport);
    window.visualViewport.addEventListener("scroll", () => { if (document.body.classList.contains("chat-open")) window.scrollTo(0, 0); });
  }
  window.addEventListener("resize", fitViewport);
  if (window.ResizeObserver) new ResizeObserver(fitViewport).observe($("composer"));
  fitViewport();

  window.addEventListener("online", () => {
    state.retries = 0;
    retryDelay = 1000;
    if (state.me) resumeConnection();
    else if ($("view-pair").classList.contains("hidden")) boot();
  });
  window.addEventListener("offline", () => { if (state.es) state.es.close(); state.es = null; if (state.me) setConn("offline", "network"); });
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) {
      resumeConnection();
      if (screen.open) { screen.gaveUp = false; screen.retries = 0; applyView(state.pendingOpen && !!state.detail && pendingKeys(state.detail).length > 0); }
    } else {
      stopTick();
      if (screen.ctrl) screenDisconnect("paused");
    }
  });
  window.addEventListener("pagehide", () => { if (state.es) state.es.close(); state.es = null; if (screen.ctrl) screenDisconnect("paused"); });
  window.addEventListener("pageshow", (e) => { if (e.persisted) resumeConnection(); });
  if ("serviceWorker" in navigator) {
    const hadController = !!navigator.serviceWorker.controller;
    navigator.serviceWorker.register("/sw.js", { scope: "/", updateViaCache: "none" }).then((reg) => {
      if (!reg || typeof reg.update !== "function") return;
      document.addEventListener("visibilitychange", () => { if (!document.hidden) Promise.resolve(reg.update()).catch(() => {}); });
    }).catch(() => {});
    // A new worker took over: offer a reload instead of forcing one, so a
    // half-typed answer is never lost.
    navigator.serviceWorker.addEventListener("controllerchange", () => { if (hadController) $("update-bar").classList.remove("hidden"); });
  }
  applyTheme();
  applyI18n();
  boot();
})();
