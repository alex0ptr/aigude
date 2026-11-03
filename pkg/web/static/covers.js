// Draws the generated covers: every <canvas class="cv"> becomes a vaporwave scene. The palette comes from
// data-cast (the CastProfile id), so the Episodes of a show look related; the composition (sun, mountains, grid)
// comes from data-cast and data-ep together, so every Episode looks different. Same input, same picture.
(function () {
  "use strict";

  // Ten colour families, kept far apart: pink/violet, electric blue, sunset, mint, yellow/indigo, teal,
  // magenta/lime, pastel, crimson, ice.
  var PALETTES = [
    { top: "#12032e", mid: "#5a1a9a", glow: "#ff2e97", sunA: "#ffd36b", sunB: "#ff2e97", grid: "#19f0ff", mtn: "#1a0540" },
    { top: "#020b3a", mid: "#0a3fd0", glow: "#4db8ff", sunA: "#ffffff", sunB: "#7ad7ff", grid: "#ff2e97", mtn: "#031052" },
    { top: "#1e0630", mid: "#b3224e", glow: "#ff8a1f", sunA: "#fff08a", sunB: "#ff4a1c", grid: "#b66bff", mtn: "#2a0a2e" },
    { top: "#02221f", mid: "#0b7a6a", glow: "#2cffb3", sunA: "#f4ffb0", sunB: "#20e6a8", grid: "#ff4fb0", mtn: "#03322d" },
    { top: "#0a0536", mid: "#3b2bb0", glow: "#ffe14d", sunA: "#fff7b0", sunB: "#ffcf1f", grid: "#19f0ff", mtn: "#120a52" },
    { top: "#021a2e", mid: "#0b6b8a", glow: "#19f0ff", sunA: "#fff3a8", sunB: "#ff6ec7", grid: "#ff2e97", mtn: "#032a3e" },
    { top: "#1a0020", mid: "#7a0f6e", glow: "#ff2fd6", sunA: "#ffb3f2", sunB: "#ff2fd6", grid: "#b6ff3c", mtn: "#260033" },
    { top: "#2b1b5c", mid: "#8e5ad6", glow: "#ffb3a7", sunA: "#fff1d6", sunB: "#ff9ec4", grid: "#fff1d6", mtn: "#3b2478" },
    { top: "#14000f", mid: "#7a0a2a", glow: "#ff3355", sunA: "#ffd0a0", sunB: "#ff2244", grid: "#ffb02e", mtn: "#220012" },
    { top: "#0b0824", mid: "#4a3aa8", glow: "#c9b8ff", sunA: "#ffffff", sunB: "#b69cff", grid: "#6ef3ff", mtn: "#150f40" }
  ];

  function hashStr(str) {
    var h = 2166136261 >>> 0;
    for (var i = 0; i < str.length; i++) {
      h ^= str.charCodeAt(i);
      h = Math.imul(h, 16777619);
    }
    return h >>> 0;
  }

  // rng returns a seeded random number generator (mulberry32).
  function rng(seed) {
    var a = seed >>> 0;
    return function () {
      a |= 0;
      a = (a + 0x6D2B79F5) | 0;
      var t = Math.imul(a ^ (a >>> 15), 1 | a);
      t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
      return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
  }

  function drawCover(cv) {
    var dpr = Math.min(window.devicePixelRatio || 1, 2);
    var w = cv.clientWidth, h = cv.clientHeight;
    if (!w || !h) {
      return;
    }
    cv.width = Math.round(w * dpr);
    cv.height = Math.round(h * dpr);
    var g = cv.getContext("2d");
    g.setTransform(dpr, 0, 0, dpr, 0, 0);

    var pal = PALETTES[hashStr(cv.dataset.cast) % PALETTES.length];
    var r = rng(hashStr(cv.dataset.cast + "|" + cv.dataset.ep));
    var m = Math.min(w, h);
    var horizon = h * (0.56 + r() * 0.1);
    var cx = w * (0.3 + r() * 0.4);
    var sunR = m * (0.2 + r() * 0.1);
    var sunY = horizon - sunR * (0.1 + r() * 0.9);
    var sunStyle = r();

    var sky = g.createLinearGradient(0, 0, 0, horizon);
    sky.addColorStop(0, pal.top);
    sky.addColorStop(0.62, pal.mid);
    sky.addColorStop(1, pal.glow);
    g.fillStyle = sky;
    g.fillRect(0, 0, w, horizon);

    var stars = Math.round(10 + r() * 26);
    for (var i = 0; i < stars; i++) {
      var sz = (r() < 0.2 ? 1.7 : 0.9) * Math.max(1, w / 320);
      g.fillStyle = "rgba(255,255,255," + (0.3 + 0.5 * r()).toFixed(2) + ")";
      g.fillRect(r() * w, r() * horizon * 0.7, sz, sz);
    }

    if (r() < 0.35) {
      var pr = m * (0.05 + r() * 0.04);
      var px = cx > w / 2 ? w * (0.1 + r() * 0.2) : w * (0.7 + r() * 0.2);
      var py = horizon * (0.18 + r() * 0.25);
      var pg = g.createLinearGradient(px, py - pr, px, py + pr);
      pg.addColorStop(0, pal.sunA);
      pg.addColorStop(1, pal.mid);
      g.fillStyle = pg;
      g.beginPath();
      g.arc(px, py, pr, 0, Math.PI * 2);
      g.fill();
      g.strokeStyle = pal.grid;
      g.lineWidth = Math.max(1, m / 150);
      g.beginPath();
      g.ellipse(px, py, pr * 1.9, pr * 0.45, -0.35, 0, Math.PI * 2);
      g.stroke();
    }

    // The sun is drawn on its own layer so that the cut-out stripes are really transparent.
    var off = document.createElement("canvas");
    off.width = cv.width;
    off.height = cv.height;
    var o = off.getContext("2d");
    o.setTransform(dpr, 0, 0, dpr, 0, 0);
    var sg = o.createLinearGradient(0, sunY - sunR, 0, sunY + sunR);
    sg.addColorStop(0, pal.sunA);
    sg.addColorStop(1, pal.sunB);
    o.fillStyle = sg;
    o.beginPath();
    o.arc(cx, sunY, sunR, 0, Math.PI * 2);
    o.fill();
    o.globalCompositeOperation = "destination-out";
    var bands = 6 + Math.floor(r() * 3);
    if (sunStyle < 0.5) {
      for (var b = 0; b < bands; b++) {
        var t = b / bands;
        o.fillRect(cx - sunR, sunY + sunR * (0.08 + t * 0.9), sunR * 2, sunR * (0.025 + 0.075 * t));
      }
    } else if (sunStyle > 0.78) {
      for (var s = 0; s < 14; s++) {
        o.fillRect(cx - sunR, sunY - sunR + (s / 14) * sunR * 2, sunR * 2, sunR * 0.045);
      }
    }
    g.save();
    g.shadowColor = pal.sunB;
    g.shadowBlur = sunR * 0.9 * dpr;
    g.drawImage(off, 0, 0, w, h);
    g.restore();

    var layers = Math.floor(r() * 4);
    for (var l = 0; l < layers; l++) {
      var amp = horizon * (l === 0 ? 0.24 : 0.13);
      var x = 0, up = true;
      g.beginPath();
      g.moveTo(0, horizon);
      g.lineTo(0, horizon - amp * r() * 0.4);
      var step = w / (7 + Math.floor(r() * 5));
      while (x < w) {
        x += step * (0.6 + r() * 0.8);
        g.lineTo(Math.min(x, w), horizon - amp * (up ? 0.5 + r() * 0.5 : r() * 0.35));
        up = !up;
      }
      g.lineTo(w, horizon);
      g.closePath();
      g.fillStyle = pal.mtn;
      g.fill();
      g.strokeStyle = pal.glow;
      g.lineWidth = Math.max(1, m / 200);
      g.globalAlpha = 0.65;
      g.stroke();
      g.globalAlpha = 1;
    }

    var floor = g.createLinearGradient(0, horizon, 0, h);
    floor.addColorStop(0, pal.mtn);
    floor.addColorStop(1, "#0d0221");
    g.fillStyle = floor;
    g.fillRect(0, horizon, w, h - horizon);

    g.save();
    g.beginPath();
    g.rect(0, horizon, w, h - horizon);
    g.clip();
    g.strokeStyle = pal.grid;
    g.lineWidth = Math.max(0.6, m / 260);
    g.shadowColor = pal.grid;
    g.shadowBlur = 4 * dpr;
    var vx = w / 2, fl = h - horizon;
    var spread = 0.09 + r() * 0.2;
    var rows = 6 + Math.floor(r() * 7);
    g.globalAlpha = 0.75;
    for (var c = -20; c <= 20; c++) {
      g.beginPath();
      g.moveTo(vx + c * w * 0.012, horizon);
      g.lineTo(vx + c * w * spread, h);
      g.stroke();
    }
    for (var k = 1; k <= rows; k++) {
      var tt = Math.pow(k / rows, 2.2);
      g.globalAlpha = 0.3 + 0.7 * tt;
      var y = horizon + fl * tt;
      g.beginPath();
      g.moveTo(0, y);
      g.lineTo(w, y);
      g.stroke();
    }
    g.restore();

    g.save();
    g.shadowColor = pal.glow;
    g.shadowBlur = 10 * dpr;
    g.fillStyle = pal.glow;
    g.fillRect(0, horizon - 1, w, Math.max(1.5, m / 220));
    g.restore();
  }

  // Covers are drawn when they appear and again when their size changes. Pages arrive by full load, by boosted
  // navigation and by "load more", so new canvases are picked up wherever they show up in the document.
  var sizeObserver = new ResizeObserver(function (entries) {
    entries.forEach(function (entry) {
      drawCover(entry.target);
    });
  });

  function watch(root) {
    if (root.nodeType !== 1) {
      return;
    }
    var canvases = root.matches("canvas.cv") ? [root] : [];
    root.querySelectorAll("canvas.cv").forEach(function (cv) {
      canvases.push(cv);
    });
    canvases.forEach(function (cv) {
      sizeObserver.observe(cv);
    });
  }

  new MutationObserver(function (mutations) {
    mutations.forEach(function (mutation) {
      mutation.addedNodes.forEach(watch);
    });
  }).observe(document.documentElement, { childList: true, subtree: true });

  document.addEventListener("DOMContentLoaded", function () {
    watch(document.body);
  });
})();
