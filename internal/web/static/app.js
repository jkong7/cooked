const app = document.getElementById("app");
const S = { me: undefined, lobby: null, match: null, es: null, esTopic: null, clockTimer: null, promptId: null, myVote: null };

const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const share = (a, b) => (a + b ? Math.round((100 * a) / (a + b)) : 50);
const ago = (iso) => {
  const s = Math.max(0, (Date.now() - new Date(iso)) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return Math.floor(s / 60) + "m ago";
  if (s < 86400) return Math.floor(s / 3600) + "h ago";
  return Math.floor(s / 86400) + "d ago";
};
const tierTag = (t) => `<span class="tier ${esc(t)}">${esc(t)}</span>`;

async function api(method, path, body, opts = {}) {
  const r = await fetch(path, { method, headers: body ? { "Content-Type": "application/json" } : {}, body: body ? JSON.stringify(body) : undefined, signal: opts.signal });
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw Object.assign(new Error(data.error || "something broke"), { status: r.status });
  return data;
}

function toast(msg, bad) {
  const t = document.createElement("div");
  t.className = "toast" + (bad ? " bad" : "");
  t.textContent = msg;
  document.getElementById("toasts").append(t);
  setTimeout(() => t.remove(), 3200);
}

function go(path) { history.pushState({}, "", path); route(); }
document.addEventListener("click", (e) => {
  const a = e.target.closest("a[data-link]");
  if (a) { e.preventDefault(); go(a.getAttribute("href")); }
});
window.addEventListener("popstate", route);

function listen(topic, url, onEvent) {
  if (S.esTopic === topic) return;
  if (S.es) S.es.close();
  S.es = new EventSource(url);
  S.esTopic = topic;
  S.es.onmessage = (e) => onEvent(JSON.parse(e.data));
}
function unlisten() { if (S.es) S.es.close(); S.es = null; S.esTopic = null; }

async function loadMe() {
  if (S.me !== undefined) return S.me;
  try { S.me = await api("GET", "/api/me"); } catch { S.me = null; }
  return S.me;
}

function sheet(html, onOpen, onClose) {
  const bg = document.createElement("div"); bg.className = "sheet-bg";
  const el = document.createElement("div"); el.className = "sheet"; el.innerHTML = html;
  const close = () => { bg.remove(); el.remove(); };
  bg.onclick = () => { close(); onClose && onClose(); };
  document.body.append(bg, el);
  onOpen && onOpen(el, close);
  return close;
}

function ensureMe() {
  if (S.me) return Promise.resolve(S.me);
  return new Promise((resolve) => {
    sheet(`<h3>Pick your debate name</h3><p class="muted small">It shows up on the leaderboard and in every clip.</p>
      <input class="field" id="nm" maxlength="20" placeholder="Name" autocomplete="nickname">
      <label class="check"><input type="checkbox" id="adult"> I'm 18 or older. I'll argue the take, not the person.</label>
      <button class="btn" id="go">Let me cook</button>`, (el, close) => {
      const nm = el.querySelector("#nm"); nm.focus();
      const submit = async () => {
        try {
          S.me = await api("POST", "/api/me", { name: nm.value, adult: el.querySelector("#adult").checked });
          close(); resolve(S.me);
        } catch (e) { toast(e.message, true); }
      };
      el.querySelector("#go").onclick = submit;
      nm.onkeydown = (e) => e.key === "Enter" && submit();
    }, () => resolve(null));
  });
}

function header() {
  const me = S.me ? `<a class="me" data-link href="/u/${S.me.id}">${esc(S.me.name)} ${tierTag(S.me.tier)} <span class="muted">${Math.round(S.me.rating)}</span></a>` : `<button class="me" id="signin">Get in</button>`;
  return `<div class="top"><a class="logo" data-link href="/">cooked</a>${me}</div>`;
}
function wireHeader() {
  const b = document.getElementById("signin");
  if (b) b.onclick = async () => { await ensureMe(); route(); };
}

function liveCard(m, watching) {
  const pa = share(m.crowd_a, m.crowd_b);
  const status = m.status === "live" ? `<span class="b">● LIVE</span> · round ${Math.min(3, Math.floor(m.turn / 2) + 1)}/3` : m.status === "voting" ? `<span class="fire">VOTING</span>` : `<span class="fire">JUDGING</span>`;
  return `<a class="card" data-link href="/m/${m.code}"><div class="small muted">${status}${watching ? ` · ${watching} watching` : ""}</div>
    <div class="p" style="margin-top:6px">“${esc(m.prompt)}”</div>
    <div class="vs"><span class="a">${esc(m.a.name)}</span><span class="b">${esc(m.b ? m.b.name : "?")}</span></div>
    <div class="tug"><span style="width:${pa}%"></span></div></a>`;
}

function recentCard(m) {
  return `<a class="card" data-link href="/m/${m.code}"><div class="p">“${esc(m.prompt)}”</div>
    <div class="small" style="margin-top:6px"><b class="fire">${esc(m.headline || "Verdict in")}</b> <span class="muted">· ${m.decided_by === "crowd" ? "crowd vote" : "judge"} · ${ago(m.ended_at)}</span></div></a>`;
}

async function lobby() {
  const L = (S.lobby = await api("GET", "/api/lobby"));
  const f = L.prompts.find((p) => p.id === S.promptId) || L.featured;
  S.promptId = f.id;
  document.title = "cooked: say it with your chest";
  const mins = Math.max(1, Math.round((new Date(L.next_rotation) - Date.now()) / 60000));
  app.innerHTML = `<div class="wrap">${header()}
    <div class="hero">
      <div class="kicker"><span class="dot"></span>${f.id === L.featured.id ? `Live take · next one in ${mins}m` : "Your pick"}</div>
      <div class="take">“${esc(f.text)}”</div>
      <div class="muted">Pick a side. We find someone who disagrees. Three rounds, 45 seconds each. The crowd votes, an AI judges.</div>
      <div class="sides"><button class="side a" data-side="a">AGREE</button><button class="side b" data-side="b">DISAGREE</button></div>
      <button class="btn ghost" id="challenge">Challenge a friend with your own take</button>
    </div>
    <h2>Or pick another take</h2>
    <div class="chips">${L.prompts.map((p) => `<button class="chip ${p.id === f.id ? "on" : ""}" data-p="${p.id}">${esc(p.text)}</button>`).join("")}</div>
    <h2>Live now ${L.live.length ? `· ${L.live.length}` : ""}</h2>
    ${L.live.length ? L.live.map((m) => liveCard(m, L.watching[m.code])).join("") : `<div class="empty">Nobody's fighting right now. Be the first.</div>`}
    <h2>Leaderboard</h2>
    <div class="card list">${L.leaderboard.length ? L.leaderboard.map((u, i) => `<a class="it" data-link href="/u/${u.id}"><span class="rank">${i + 1}</span>
      <span class="grow"><b>${esc(u.name)}</b> ${tierTag(u.tier)}</span><span class="num">${Math.round(u.rating)}</span></a>`).join("") : `<div class="empty">No ranked debaters yet.</div>`}</div>
    <h2>Fresh verdicts</h2>
    ${L.recent.length ? L.recent.map(recentCard).join("") : `<div class="empty">No verdicts yet.</div>`}
    <p class="small muted" style="margin-top:24px">18+. Argue ideas, not identities. Slurs and personal info get blocked; anyone can report a debate.</p>
  </div>`;
  wireHeader();
  app.querySelectorAll("[data-side]").forEach((b) => (b.onclick = () => queue(f, b.dataset.side)));
  app.querySelectorAll("[data-p]").forEach((b) => (b.onclick = () => { S.promptId = Number(b.dataset.p); lobby(); window.scrollTo({ top: 0, behavior: "smooth" }); }));
  document.getElementById("challenge").onclick = challenge;
  listen("lobby", "/api/lobby/events", () => { clearTimeout(S.lobbyT); S.lobbyT = setTimeout(() => location.pathname === "/" && lobby(), 400); });
}

let queueAbort;
async function queue(prompt, side) {
  if (!(await ensureMe())) return;
  queueAbort = new AbortController();
  const lines = ["Finding someone who's wrong…", "Scanning group chats for a hater…", "Warming up the judge…", "Looking for an opponent with confidence…"];
  let i = 0;
  app.innerHTML = `<div class="wrap">${header()}<div class="searching"><div class="spinner"></div>
    <div class="take" style="font-size:1.6rem">“${esc(prompt.text)}”</div>
    <p>You're arguing <b class="${side}">${side === "a" ? "AGREE" : "DISAGREE"}</b></p>
    <p class="muted" id="ql">${lines[0]}</p><p class="small muted">If nobody bites in a few seconds, you fight cookbot.</p>
    <button class="btn ghost" id="cancel" style="margin-top:20px">Cancel</button></div></div>`;
  const spin = setInterval(() => { const el = document.getElementById("ql"); if (el) el.textContent = lines[++i % lines.length]; }, 2200);
  document.getElementById("cancel").onclick = () => queueAbort.abort();
  try {
    const r = await api("POST", "/api/queue", { prompt_id: prompt.id, side }, { signal: queueAbort.signal });
    clearInterval(spin);
    go(`/m/${r.code}`);
  } catch (e) {
    clearInterval(spin);
    if (e.name !== "AbortError") toast(e.message, true);
    lobby();
  }
}

async function challenge() {
  if (!(await ensureMe())) return;
  sheet(`<h3>Challenge a friend</h3><p class="muted small">Write a take. You argue AGREE. Send the link and whoever opens it argues against you.</p>
    <input class="field" id="tk" maxlength="140" placeholder="Marcus can't cook.">
    <div style="height:12px"></div><button class="btn" id="mk">Create challenge</button>`, (el, close) => {
    const tk = el.querySelector("#tk"); tk.focus();
    el.querySelector("#mk").onclick = async () => {
      try { const m = await api("POST", "/api/challenges", { prompt: tk.value }); close(); go(`/m/${m.code}`); }
      catch (e) { toast(e.message, true); }
    };
  });
}

function mySide(m) {
  if (!S.me) return "";
  if (m.a.id === S.me.id) return "a";
  if (m.b && m.b.id === S.me.id) return "b";
  return "";
}

function tick() {
  clearInterval(S.clockTimer);
  const el = document.getElementById("clock");
  if (!el || !S.match) return;
  const upd = () => {
    const m = S.match;
    if (m.status !== "live" && m.status !== "voting") { el.textContent = m.status === "judging" ? "⚖️" : ""; return; }
    const s = Math.max(0, Math.ceil((new Date(m.deadline) - Date.now()) / 1000));
    el.textContent = s + "s";
    el.classList.toggle("hot", s <= 10);
  };
  upd();
  S.clockTimer = setInterval(upd, 250);
}

function bubbles(m) {
  const out = m.turns.map((t) => {
    const p = t.side === "a" ? m.a : m.b;
    return `<div class="bubble ${t.side} ${t.text ? "" : "choke"}"><div class="who">${esc(p ? p.name : "?")}</div>${t.text ? esc(t.text) : "ran out of time 💀"}</div>`;
  });
  if (m.status === "live") {
    const side = m.turn % 2 === 0 ? "a" : "b";
    const p = side === "a" ? m.a : m.b;
    if (side !== mySide(m) && p) out.push(`<div class="typing ${side}">${esc(p.name)} ${p.bot ? "is typing…" : "is up…"}</div>`);
  }
  return out.join("");
}

function renderMatch() {
  const m = S.match;
  const me = mySide(m);
  const pa = m.status === "done" && m.decided_by === "judge" && m.crowd_a + m.crowd_b === 0 ? m.score_a : share(m.crowd_a, m.crowd_b);
  const b = m.b || { name: "waiting…", tier: "" };
  const round = Math.min(3, Math.floor(m.turn / 2) + 1);
  const stage = m.status === "live" ? `Round ${round} of 3` : m.status === "voting" ? "Final votes" : m.status === "judging" ? "The judge is deliberating" : m.status === "waiting" ? "Waiting for an opponent" : "Verdict";
  let dock = "";
  if (m.status === "waiting") {
    dock = me === "a" ? `<button class="btn" id="invite">Send the challenge link</button><p class="small muted" style="text-align:center">Whoever opens it first argues against you.</p>`
      : `<button class="btn" id="accept">Take the other side</button>`;
  } else if (m.status === "live" && me && ((m.turn % 2 === 0) === (me === "a"))) {
    dock = `<div class="composer"><textarea id="say" rows="2" maxlength="280" placeholder="Make your point. 280 characters."></textarea><button class="send" id="send">Send</button></div><div class="count" id="count">0/280</div>`;
  } else if ((m.status === "live" || m.status === "voting") && !me) {
    dock = `<div class="votes"><button class="vbtn a ${S.myVote === "a" ? "on" : ""}" data-v="a">${esc(m.a.name)} is right</button><button class="vbtn b ${S.myVote === "b" ? "on" : ""}" data-v="b">${esc(b.name)} is right</button></div>
      <div class="crowd"><span class="a">${m.crowd_a} votes</span><span>${m.watching || 0} watching</span><span class="b">${m.crowd_b} votes</span></div>`;
  } else if (m.status === "live" && me) {
    dock = `<p class="muted small" style="text-align:center">Their turn. Think of something mean (about the take).</p>`;
  } else if (m.status === "voting" && me) {
    dock = `<p class="muted small" style="text-align:center">The crowd is voting. You can't vote on your own debate.</p>`;
  }
  let verdict = "";
  if (m.status === "done") {
    const won = me && m.winner === me;
    const delta = me ? (won ? `+${Math.round(m.delta)}` : `−${Math.round(m.delta)}`) : "";
    verdict = `<div class="verdict"><div class="small muted">${m.decided_by === "crowd" ? `Crowd ${pa}–${100 - pa}` : `Judge ${m.score_a}–${100 - m.score_a}`}</div>
      <div class="head">${esc(m.headline)}</div><div class="reason">${esc(m.reason)}</div>
      <div class="roast"><b class="a">${esc(m.a.name)}:</b> ${esc(m.roast_a)}</div><div class="roast"><b class="b">${esc(b.name)}:</b> ${esc(m.roast_b)}</div>
      ${me ? `<div class="delta" style="color:${won ? "var(--ok)" : "var(--b)"}">${won ? "You cooked." : "You got cooked."} ${delta} rating</div>` : ""}
      <div class="row" style="margin-top:14px"><button class="btn" id="clip">Save the clip</button><button class="btn ghost" id="shr">Share</button></div>
      ${m.prompt_id ? `<button class="btn ghost" id="again" style="margin-top:10px">Run it back</button>` : ""}</div>`;
  }
  const scroll = document.querySelector(".feed") ? window.scrollY : null;
  app.innerHTML = `<div class="stage">
    <div class="stage-head"><div class="row" style="justify-content:space-between;align-items:center"><a class="logo" data-link href="/" style="font-size:1.15rem">cooked</a>
      <span class="small muted">${stage}</span><button class="small muted" id="report">Report</button></div>
      <div class="stage-take">“${esc(m.prompt)}”</div>
      <div class="players"><div class="pl"><span class="lbl a">AGREE</span><b>${esc(m.a.name)}${me === "a" ? " (you)" : ""}</b>${m.a.tier ? tierTag(m.a.tier) : ""}</div>
        <div class="clock" id="clock"></div>
        <div class="pl r"><span class="lbl b">DISAGREE</span><b>${esc(b.name)}${me === "b" ? " (you)" : ""}</b>${b.tier ? tierTag(b.tier) : ""}</div></div>
      <div class="tug"><span style="width:${pa}%"></span></div></div>
    ${verdict}
    <div class="feed" id="feed">${bubbles(m)}</div>
    <div class="dock">${dock}</div></div>`;
  tick();
  if (scroll === null || m.status === "live") window.scrollTo(0, document.body.scrollHeight);
  wireMatch(m, me);
}

function wireMatch(m, me) {
  const say = document.getElementById("say");
  if (say) {
    say.focus();
    const count = document.getElementById("count");
    say.oninput = () => (count.textContent = `${say.value.length}/280`);
    const send = async () => {
      if (!say.value.trim()) return;
      document.getElementById("send").disabled = true;
      try { S.match = await api("POST", `/api/matches/${m.code}/say`, { text: say.value }); renderMatch(); }
      catch (e) { toast(e.message, true); document.getElementById("send").disabled = false; }
    };
    document.getElementById("send").onclick = send;
    say.onkeydown = (e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); send(); } };
  }
  app.querySelectorAll("[data-v]").forEach((btn) => (btn.onclick = async () => {
    if (!(await ensureMe())) return;
    try { S.myVote = btn.dataset.v; S.match = await api("POST", `/api/matches/${m.code}/vote`, { side: btn.dataset.v }); renderMatch(); }
    catch (e) { toast(e.message, true); }
  }));
  const accept = document.getElementById("accept");
  if (accept) accept.onclick = async () => {
    if (!(await ensureMe())) return;
    try { S.match = await api("POST", `/api/matches/${m.code}/accept`); renderMatch(); } catch (e) { toast(e.message, true); }
  };
  const invite = document.getElementById("invite");
  if (invite) invite.onclick = () => shareLink(m, `I bet you can't argue against this: “${m.prompt}”`);
  const shr = document.getElementById("shr");
  if (shr) shr.onclick = () => shareLink(m, `${m.headline} on cooked: “${m.prompt}”`);
  const clip = document.getElementById("clip");
  if (clip) clip.onclick = () => makeClip(m, clip);
  const again = document.getElementById("again");
  if (again) again.onclick = () => { const p = S.lobby?.prompts?.find((x) => x.id === m.prompt_id) || { id: m.prompt_id, text: m.prompt }; queue(p, me || "a"); };
  document.getElementById("report").onclick = async () => {
    if (!(await ensureMe())) return;
    sheet(`<h3>Report this debate</h3><input class="field" id="why" maxlength="200" placeholder="What happened?"><div style="height:12px"></div><button class="btn" id="rp">Send report</button>`, (el, close) => {
      el.querySelector("#rp").onclick = async () => {
        try { await api("POST", `/api/matches/${m.code}/report`, { reason: el.querySelector("#why").value || "reported" }); close(); toast("Reported. Thanks."); }
        catch (e) { toast(e.message, true); }
      };
    });
  };
}

async function shareLink(m, text) {
  const url = `${location.origin}/m/${m.code}`;
  if (navigator.share) { try { await navigator.share({ text, url }); return; } catch { } }
  try { await navigator.clipboard.writeText(`${text} ${url}`); toast("Link copied."); } catch { prompt("Copy this link", url); }
}

async function makeClip(m, btn) {
  btn.disabled = true;
  const old = btn.textContent;
  btn.textContent = "Rendering…";
  try {
    const file = await window.CookedClip.render(m);
    if (navigator.canShare && navigator.canShare({ files: [file] })) {
      try { await navigator.share({ files: [file], text: m.headline }); return; } catch { }
    }
    const a = document.createElement("a");
    a.href = URL.createObjectURL(file);
    a.download = file.name;
    a.click();
    toast("Clip saved. Post it.");
  } catch (e) { toast("This browser can't record clips. Try Chrome or Safari.", true); }
  finally { btn.disabled = false; btn.textContent = old; }
}

async function matchPage(code) {
  try { S.match = await api("GET", `/api/matches/${code}`); }
  catch { app.innerHTML = `<div class="wrap">${header()}<div class="empty">That debate doesn't exist.</div><a class="btn ghost" data-link href="/">Back to the lobby</a></div>`; return; }
  document.title = `“${S.match.prompt}” · cooked`;
  S.myVote = null;
  renderMatch();
  listen(`m:${code}`, `/api/matches/${code}/events`, (ev) => {
    if (ev.type === "vote") { Object.assign(S.match, ev.data); renderMatch(); return; }
    const watching = S.match.watching;
    S.match = { ...ev.data, watching };
    if (ev.type === "verdict") toast(ev.data.headline);
    if (ev.type === "choke") toast("Someone froze. Turn skipped.");
    renderMatch();
  });
}

async function profile(id) {
  let p;
  try { p = await api("GET", `/api/users/${id}`); } catch { return go("/"); }
  const u = p.user;
  app.innerHTML = `<div class="wrap">${header()}
    <div class="hero"><div class="take" style="font-size:2.4rem">${esc(u.name)}</div>${tierTag(u.tier)}
      <div class="row" style="margin-top:16px"><div class="card grow"><div class="small muted">Rating</div><div class="num" style="font-size:1.6rem">${Math.round(u.rating)}</div></div>
      <div class="card grow"><div class="small muted">Record</div><div class="num" style="font-size:1.6rem"><span class="a">${u.wins}</span>–<span class="b">${u.losses}</span></div></div></div></div>
    <h2>Debates</h2>${p.history.length ? p.history.map((m) => {
      const side = m.a.id === u.id ? "a" : "b";
      const won = m.winner === side;
      return `<a class="card" data-link href="/m/${m.code}"><div class="p">“${esc(m.prompt)}”</div><div class="small" style="margin-top:6px"><b class="${won ? "a" : "b"}">${won ? "W" : "L"}</b> <span class="muted">vs ${esc(side === "a" ? m.b?.name : m.a.name)} · ${ago(m.ended_at)}</span></div></a>`;
    }).join("") : `<div class="empty">No debates yet.</div>`}</div>`;
  wireHeader();
}

async function route() {
  await loadMe();
  clearInterval(S.clockTimer);
  const p = location.pathname;
  let m;
  if ((m = p.match(/^\/m\/([a-z0-9]+)$/))) return matchPage(m[1]);
  if ((m = p.match(/^\/u\/(\d+)$/))) { unlisten(); return profile(m[1]); }
  lobby();
}

route();
