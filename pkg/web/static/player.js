// The player of an Episode page. The page ships a plain <audio controls> element; this script hides its controls and
// shows its own (play/pause, jumps, Chapter buttons, speed, a timeline split into Chapters) on top of the same element.
// Everything is delegated from the document, so it works for every Episode page however the page arrived (full load
// or boosted navigation), and the Chapter links keep working without the rest.
(function () {
  "use strict";

  var RATES = [1, 1.25, 1.5, 2];

  function root() {
    return document.querySelector("[data-player]");
  }

  function clock(seconds) {
    seconds = Math.max(0, Math.round(seconds));
    return Math.floor(seconds / 60) + ":" + String(seconds % 60).padStart(2, "0");
  }

  function chapters(player) {
    return Array.prototype.map.call(player.querySelectorAll("[data-chapter-start]"), function (link) {
      return { start: parseFloat(link.dataset.chapterStart), title: link.textContent.trim() };
    });
  }

  // length is the length of the Episode in seconds; the browser reports it once it has read the file.
  function length(player, audio) {
    return isFinite(audio.duration) && audio.duration > 0 ? audio.duration : parseFloat(player.dataset.duration);
  }

  function chapterAt(list, t) {
    var index = 0;
    list.forEach(function (c, i) {
      if (c.start <= t) {
        index = i;
      }
    });
    return index;
  }

  // enhance swaps the browser controls for ours and lays out the timeline, once per page.
  function enhance(player, audio) {
    if (player.dataset.ready) {
      return;
    }
    player.dataset.ready = "true";
    audio.removeAttribute("controls");
    player.querySelector(".player-ui").hidden = false;
    var timeline = player.querySelector(".timeline");
    var list = chapters(player);
    var total = length(player, audio);
    list.forEach(function (c, i) {
      var end = i + 1 < list.length ? list[i + 1].start : total;
      var seg = document.createElement("i");
      seg.className = "seg";
      seg.style.setProperty("--len", String(Math.max(end - c.start, 1)));
      seg.dataset.start = String(c.start);
      seg.dataset.end = String(end);
      seg.title = clock(c.start) + " " + c.title;
      timeline.appendChild(seg);
    });
    var head = document.createElement("i");
    head.className = "head";
    timeline.appendChild(head);
    player.querySelector("[data-total]").textContent = clock(total);
  }

  function update() {
    var player = root();
    if (!player) {
      return;
    }
    var audio = player.querySelector("audio");
    enhance(player, audio);
    var t = audio.currentTime || 0;
    var total = length(player, audio);
    var list = chapters(player);
    var index = chapterAt(list, t);

    player.classList.toggle("is-playing", !audio.paused && !audio.ended);
    player.querySelector("[data-clock]").textContent = clock(t);
    player.querySelector("[data-total]").textContent = clock(total);
    player.querySelector("[data-chapter-now]").textContent = list.length ? "Chapter " + (index + 1) + " of " + list.length + " · " + list[index].title : "";
    player.querySelector("[data-rate]").textContent = String(audio.playbackRate) + "×";

    var timeline = player.querySelector(".timeline");
    timeline.setAttribute("aria-valuemax", String(Math.round(total)));
    timeline.setAttribute("aria-valuenow", String(Math.round(t)));
    timeline.setAttribute("aria-valuetext", clock(t) + " of " + clock(total));
    var head = timeline.querySelector(".head");
    timeline.querySelectorAll(".seg").forEach(function (seg) {
      var start = parseFloat(seg.dataset.start), end = parseFloat(seg.dataset.end);
      var done = Math.min(1, Math.max(0, (t - start) / (end - start)));
      seg.style.setProperty("--p", String(done));
      if (t >= start && (t < end || end >= total)) {
        head.style.left = seg.offsetLeft + done * seg.offsetWidth + "px";
      }
    });
    player.querySelectorAll(".chapters li").forEach(function (li, i) {
      li.classList.toggle("is-current", i === index);
      li.classList.toggle("is-past", i < index);
    });
  }

  function seek(audio, t) {
    var total = length(root(), audio);
    audio.currentTime = Math.min(total, Math.max(0, t));
    update();
  }

  function jumpChapter(audio, direction) {
    var list = chapters(root());
    var index = chapterAt(list, audio.currentTime);
    if (direction < 0) {
      var target = audio.currentTime - list[index].start > 3 || index === 0 ? index : index - 1;
      seek(audio, list[target].start);
    } else if (index < list.length - 1) {
      seek(audio, list[index + 1].start);
    }
  }

  function toggle(audio) {
    if (audio.paused) {
      audio.play().catch(function () {});
    } else {
      audio.pause();
    }
  }

  // timeAt turns a pointer position on the timeline into a moment in the Episode.
  function timeAt(timeline, clientX) {
    var segs = timeline.querySelectorAll(".seg");
    for (var i = 0; i < segs.length; i++) {
      var rect = segs[i].getBoundingClientRect();
      if (clientX <= rect.right + 1.5 || i === segs.length - 1) {
        var start = parseFloat(segs[i].dataset.start), end = parseFloat(segs[i].dataset.end);
        return start + Math.min(1, Math.max(0, (clientX - rect.left) / rect.width)) * (end - start);
      }
    }
    return 0;
  }

  document.addEventListener("click", function (event) {
    var link = event.target.closest("[data-chapter-start]");
    if (link) {
      event.preventDefault();
      var audio = document.getElementById("player");
      audio.currentTime = parseFloat(link.dataset.chapterStart);
      audio.play().catch(function () {});
      return;
    }
    var button = event.target.closest("[data-action]");
    if (!button || !root()) {
      return;
    }
    var media = document.getElementById("player");
    switch (button.dataset.action) {
      case "toggle": toggle(media); break;
      case "back": seek(media, media.currentTime - 15); break;
      case "forward": seek(media, media.currentTime + 30); break;
      case "previous-chapter": jumpChapter(media, -1); break;
      case "next-chapter": jumpChapter(media, 1); break;
      case "rate":
        var rate = RATES[(RATES.indexOf(media.playbackRate) + 1) % RATES.length];
        // The default rate as well, because some browsers fall back to it when playback starts.
        media.defaultPlaybackRate = rate;
        media.playbackRate = rate;
        update();
        break;
    }
  });

  var dragging = false;
  document.addEventListener("pointerdown", function (event) {
    var timeline = event.target.closest(".timeline");
    if (!timeline) {
      return;
    }
    dragging = true;
    timeline.setPointerCapture(event.pointerId);
    seek(document.getElementById("player"), timeAt(timeline, event.clientX));
  });
  document.addEventListener("pointermove", function (event) {
    var timeline = event.target.closest && event.target.closest(".timeline");
    if (dragging && timeline) {
      seek(document.getElementById("player"), timeAt(timeline, event.clientX));
    }
  });
  document.addEventListener("pointerup", function () {
    dragging = false;
  });

  document.addEventListener("keydown", function (event) {
    if (!root() || event.metaKey || event.ctrlKey || event.altKey) {
      return;
    }
    var audio = document.getElementById("player");
    var onTimeline = event.target.closest && event.target.closest(".timeline");
    if (onTimeline && event.key === "ArrowRight") {
      event.preventDefault();
      seek(audio, audio.currentTime + 5);
    } else if (onTimeline && event.key === "ArrowLeft") {
      event.preventDefault();
      seek(audio, audio.currentTime - 5);
    }
    var tag = event.target.tagName;
    if (tag === "BUTTON" || tag === "SELECT" || tag === "INPUT" || tag === "A" || onTimeline) {
      return;
    }
    if (event.key === " ") {
      event.preventDefault();
      toggle(audio);
    } else if (event.key === "j") {
      jumpChapter(audio, -1);
    } else if (event.key === "k") {
      jumpChapter(audio, 1);
    }
  });

  // Media events do not bubble, so they are caught on the way down.
  ["timeupdate", "play", "pause", "ended", "loadedmetadata", "durationchange", "ratechange"].forEach(function (name) {
    document.addEventListener(name, update, true);
  });
  window.addEventListener("resize", update);
  document.addEventListener("DOMContentLoaded", update);
  document.addEventListener("htmx:afterSettle", update);
})();
