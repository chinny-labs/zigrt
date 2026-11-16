// The PIN pad. Without this script the plain PIN field and Go button work.
(function () {
  "use strict";
  var form = document.querySelector("[data-pin-form]");
  if (!form) return;
  var input = form.querySelector("input[name=pin]");
  var pad = form.querySelector("[data-pin-pad]");
  var field = form.querySelector("[data-pin-field]");
  var dots = document.querySelector("[data-pin-dots]");
  if (!input || !pad || !field || !dots) return;
  var max = parseInt(input.getAttribute("maxlength"), 10) || 6;

  // Show the pad and dots; keep the real field for the form and for
  // keyboards, but out of sight.
  pad.hidden = false;
  dots.hidden = false;
  field.classList.add("visually-hidden");

  function draw() {
    var spans = dots.children;
    for (var i = 0; i < spans.length; i++) spans[i].classList.toggle("on", i < input.value.length);
  }
  function press(key) {
    if (key === "del") input.value = input.value.slice(0, -1);
    else if (input.value.length < max) input.value += key;
    draw();
  }
  pad.addEventListener("click", function (e) {
    var b = e.target.closest("[data-key]");
    if (!b) return;
    press(b.getAttribute("data-key"));
  });
  input.addEventListener("input", function () {
    input.value = input.value.replace(/\D/g, "").slice(0, max);
    draw();
  });
  draw();
})();

// Worksheet page: a Print button, and options that apply as soon as they
// are ticked. Without this script the Update button and the browser's own
// Print command do the same jobs.
(function () {
  "use strict";
  var print = document.querySelector("[data-print]");
  if (print) {
    print.hidden = false;
    var hint = document.querySelector("[data-print-hint]");
    if (hint) hint.hidden = true;
    print.addEventListener("click", function () { window.print(); });
  }
  var form = document.querySelector("form[data-autosubmit]");
  if (form) {
    var button = form.querySelector("[data-autosubmit-button]");
    if (button) button.hidden = true;
    form.addEventListener("change", function () { form.submit(); });
  }
})();
