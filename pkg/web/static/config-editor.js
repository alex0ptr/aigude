// Highlights the TOML of the User Config editor with Prism. The <textarea class="config-text"> stays what is edited and
// submitted; a <pre> behind it, whose text is the same, carries the colours, and the textarea's own text is made
// transparent. Without Prism or this script the editor is a plain textarea. The page changes without being loaded
// when HTMX swaps the body, so the editor is set up after every swap and after a page restored from the history.
(function () {
  "use strict";

  // done holds the textareas that are set up; a textarea of a restored page is a new element and is not in it.
  var done = new WeakSet();

  function setup() {
    var area = document.querySelector("textarea.config-text");
    if (!area || done.has(area) || !window.Prism || !Prism.languages.toml) return;
    done.add(area);

    // A restored page keeps the markup of an earlier setup but none of its listeners.
    var old = area.closest(".cfg-wrap");
    if (old) old.replaceWith(area);

    var wrap = document.createElement("div");
    wrap.className = "cfg-wrap";
    var pre = document.createElement("pre");
    pre.className = "cfg-hl";
    pre.setAttribute("aria-hidden", "true");
    var code = document.createElement("code");
    pre.appendChild(code);
    area.parentNode.insertBefore(wrap, area);
    wrap.appendChild(pre);
    wrap.appendChild(area);

    // The pre must lay out its text exactly like the textarea does.
    var style = getComputedStyle(area);
    ["paddingTop", "paddingRight", "paddingBottom", "paddingLeft", "borderTopWidth", "borderRightWidth",
      "borderBottomWidth", "borderLeftWidth", "fontFamily", "fontSize", "lineHeight", "letterSpacing", "tabSize"]
      .forEach(function (property) { pre.style[property] = style[property]; });
    area.setAttribute("wrap", "off");

    function scroll() {
      pre.scrollTop = area.scrollTop;
      pre.scrollLeft = area.scrollLeft;
    }

    function render() {
      // The extra line keeps the last line, when it is empty, as high as the textarea's.
      code.innerHTML = Prism.highlight(area.value, Prism.languages.toml, "toml") + "\n";
      scroll();
    }

    area.addEventListener("input", render);
    area.addEventListener("scroll", scroll);
    wrap.classList.add("on");
    render();
  }

  document.addEventListener("DOMContentLoaded", setup);
  document.addEventListener("htmx:afterSettle", setup);
  document.addEventListener("htmx:historyRestore", setup);
})();
