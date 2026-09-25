(() => {
  const W = 1080, H = 1920, FPS = 30;
  const C = { bg: "#0c0a09", card: "#1c1917", fg: "#fafaf9", muted: "#a8a29e", fire: "#f97316", a: "#38bdf8", b: "#f43f5e" };
  const FONT = "ui-sans-serif, system-ui, -apple-system, 'Segoe UI', sans-serif";

  const ease = (t) => 1 - Math.pow(1 - Math.min(1, Math.max(0, t)), 3);

  function wrap(ctx, text, width) {
    const words = String(text).split(/\s+/).filter(Boolean);
    const lines = [];
    let cur = "";
    for (const w of words) {
      const t = cur ? cur + " " + w : w;
      if (ctx.measureText(t).width > width && cur) { lines.push(cur); cur = w; } else cur = t;
    }
    if (cur) lines.push(cur);
    return lines;
  }

  function roundRect(ctx, x, y, w, h, r) {
    ctx.beginPath();
    ctx.moveTo(x + r, y);
    ctx.arcTo(x + w, y, x + w, y + h, r);
    ctx.arcTo(x + w, y + h, x, y + h, r);
    ctx.arcTo(x, y + h, x, y, r);
    ctx.arcTo(x, y, x + w, y, r);
    ctx.closePath();
  }

  function layout(ctx, m) {
    ctx.font = `600 40px ${FONT}`;
    return m.turns.map((t) => {
      const text = t.text || "ran out of time 💀";
      const lines = wrap(ctx, text, 720);
      return { side: t.side, lines: lines.slice(0, 6), h: 70 + lines.slice(0, 6).length * 52, choke: !t.text };
    });
  }

  function frame(ctx, m, bubbles, t, T) {
    ctx.fillStyle = C.bg;
    ctx.fillRect(0, 0, W, H);
    ctx.textBaseline = "alphabetic";

    ctx.fillStyle = C.fire;
    ctx.font = `900 64px ${FONT}`;
    ctx.fillText("cooked", 72, 150);
    ctx.fillStyle = C.muted;
    ctx.font = `700 34px ${FONT}`;
    ctx.fillText("live 1v1 hot takes", 72, 204);

    const intro = ease(t / 0.8);
    ctx.globalAlpha = intro;
    ctx.fillStyle = C.fg;
    ctx.font = `900 76px ${FONT}`;
    const takeLines = wrap(ctx, "“" + m.prompt + "”", W - 144).slice(0, 4);
    takeLines.forEach((l, i) => ctx.fillText(l, 72, 330 + i * 88));
    const namesY = 330 + takeLines.length * 88 + 60;
    ctx.font = `800 30px ${FONT}`;
    ctx.fillStyle = C.a;
    ctx.fillText("AGREE", 72, namesY);
    ctx.fillStyle = C.b;
    ctx.textAlign = "right";
    ctx.fillText("DISAGREE", W - 72, namesY);
    ctx.font = `900 50px ${FONT}`;
    ctx.fillStyle = C.fg;
    ctx.fillText(m.b ? m.b.name : "?", W - 72, namesY + 60);
    ctx.textAlign = "left";
    ctx.fillText(m.a.name, 72, namesY + 60);
    ctx.globalAlpha = 1;

    const top = namesY + 120, bottom = H - 520;
    const per = T.turns / Math.max(1, bubbles.length);
    const shown = bubbles.filter((_, i) => t >= T.intro + i * per);
    let total = shown.reduce((s, b) => s + b.h + 24, 0);
    let y = top - Math.max(0, total - (bottom - top));
    ctx.save();
    ctx.beginPath();
    ctx.rect(0, top - 10, W, bottom - top + 20);
    ctx.clip();
    shown.forEach((b, i) => {
      const age = t - (T.intro + i * per);
      const k = ease(age / 0.35);
      const w = 800;
      const x = b.side === "a" ? 72 : W - 72 - w;
      ctx.globalAlpha = k * (b.choke ? 0.55 : 1);
      if (y + b.h > top - 40) {
        ctx.fillStyle = b.side === "a" ? "#12303d" : "#3d1219";
        roundRect(ctx, x, y + (1 - k) * 30, w, b.h, 34);
        ctx.fill();
        ctx.fillStyle = b.side === "a" ? C.a : C.b;
        ctx.font = `900 26px ${FONT}`;
        ctx.fillText((b.side === "a" ? m.a.name : (m.b ? m.b.name : "")).toUpperCase(), x + 32, y + 48 + (1 - k) * 30);
        ctx.fillStyle = C.fg;
        ctx.font = `${b.choke ? "italic " : ""}600 40px ${FONT}`;
        b.lines.forEach((l, j) => ctx.fillText(l, x + 32, y + 104 + j * 52 + (1 - k) * 30));
      }
      y += b.h + 24;
      ctx.globalAlpha = 1;
    });
    ctx.restore();
    const fade = ctx.createLinearGradient(0, top - 10, 0, top + 70);
    fade.addColorStop(0, C.bg);
    fade.addColorStop(1, "rgba(12,10,9,0)");
    ctx.fillStyle = fade;
    ctx.fillRect(0, top - 10, W, 80);

    const votesTotal = m.crowd_a + m.crowd_b;
    const finalA = votesTotal ? m.crowd_a / votesTotal : (m.score_a || 50) / 100;
    const swing = ease((t - T.intro - T.turns) / T.swing);
    const wobble = t < T.intro + T.turns ? 0.5 + 0.12 * Math.sin(t * 3) : 0.5 + (finalA - 0.5) * swing;
    const barY = H - 470;
    ctx.fillStyle = C.b;
    roundRect(ctx, 72, barY, W - 144, 36, 18);
    ctx.fill();
    ctx.save();
    roundRect(ctx, 72, barY, W - 144, 36, 18);
    ctx.clip();
    ctx.fillStyle = C.a;
    ctx.fillRect(72, barY, (W - 144) * wobble, 36);
    ctx.restore();
    ctx.font = `800 30px ${FONT}`;
    ctx.fillStyle = C.a;
    ctx.fillText(Math.round(wobble * 100) + "%", 72, barY + 84);
    ctx.fillStyle = C.b;
    ctx.textAlign = "right";
    ctx.fillText(Math.round((1 - wobble) * 100) + "%", W - 72, barY + 84);
    ctx.textAlign = "left";

    const vt = t - T.intro - T.turns - T.swing * 0.6;
    if (vt > 0 && m.headline) {
      const k = ease(vt / 0.4);
      const scale = 1.3 - 0.3 * k;
      ctx.save();
      ctx.globalAlpha = k;
      ctx.translate(W / 2, H - 250);
      ctx.scale(scale, scale);
      ctx.fillStyle = C.fire;
      roundRect(ctx, -470, -110, 940, 220, 40);
      ctx.fill();
      ctx.fillStyle = "#1c0a02";
      ctx.textAlign = "center";
      ctx.font = `950 64px ${FONT}`;
      const hl = wrap(ctx, m.headline, 860).slice(0, 2);
      hl.forEach((l, i) => ctx.fillText(l, 0, -10 + (i - (hl.length - 1) / 2) * 70 + 20));
      ctx.restore();
      ctx.textAlign = "left";
      ctx.globalAlpha = k;
      ctx.fillStyle = C.muted;
      ctx.font = `700 32px ${FONT}`;
      ctx.textAlign = "center";
      ctx.fillText(m.decided_by === "crowd" ? "decided by the crowd" : "decided by the judge", W / 2, H - 90);
      ctx.textAlign = "left";
      ctx.globalAlpha = 1;
    }
  }

  function pickType() {
    const types = ["video/mp4;codecs=avc1", "video/mp4", "video/webm;codecs=vp9", "video/webm"];
    return types.find((t) => window.MediaRecorder && MediaRecorder.isTypeSupported(t));
  }

  async function render(m) {
    const type = pickType();
    const canvas = document.createElement("canvas");
    canvas.width = W;
    canvas.height = H;
    const ctx = canvas.getContext("2d");
    const bubbles = layout(ctx, m);
    const T = { intro: 1.0, turns: Math.max(3, bubbles.length * 1.2), swing: 1.6, outro: 2.4 };
    const total = T.intro + T.turns + T.swing + T.outro;
    if (!type || !canvas.captureStream) {
      frame(ctx, m, bubbles, total, T);
      const blob = await new Promise((r) => canvas.toBlob(r, "image/png"));
      return new File([blob], `cooked-${m.code}.png`, { type: "image/png" });
    }
    const stream = canvas.captureStream(FPS);
    const rec = new MediaRecorder(stream, { mimeType: type, videoBitsPerSecond: 6_000_000 });
    const chunks = [];
    rec.ondataavailable = (e) => e.data.size && chunks.push(e.data);
    const done = new Promise((r) => (rec.onstop = r));
    rec.start(250);
    const start = performance.now();
    await new Promise((resolve) => {
      const step = () => {
        const t = (performance.now() - start) / 1000;
        frame(ctx, m, bubbles, Math.min(t, total), T);
        if (t < total) requestAnimationFrame(step); else resolve();
      };
      step();
    });
    rec.stop();
    await done;
    const ext = type.startsWith("video/mp4") ? "mp4" : "webm";
    return new File([new Blob(chunks, { type: type.split(";")[0] })], `cooked-${m.code}.${ext}`, { type: type.split(";")[0] });
  }

  window.CookedClip = { render, frame, layout };
})();
