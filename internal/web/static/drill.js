// The on-screen drill (plan §5.2). The page holds every problem, drawn by
// the server; this script shows them one at a time, takes the answer from the
// number pad or the keyboard, and gives instant feedback. Its marking is only
// for that feedback: the answers are posted when the drill ends and the
// server marks them again.
(function () {
  "use strict";
  var app = document.querySelector("[data-drill]");
  var finish = document.querySelector("form[data-finish]");
  if (!app || !finish) return;

  var problems = Array.prototype.slice.call(app.querySelectorAll("[data-problem]"));
  var countEl = app.querySelector("[data-count]");
  var progress = app.querySelector("[data-progress]");
  var feedback = app.querySelector("[data-feedback]");
  var checkBtn = app.querySelector("[data-check]");
  var switchKey = app.querySelector('[data-key="switch"]');
  var soundBtn = app.querySelector("[data-sound]");
  var keys = Array.prototype.slice.call(app.querySelectorAll("[data-key]"));
  var MAX_DIGITS = 6;
  var RIGHT_PAUSE_MS = 650;

  var index = 0;
  var input = { main: "", rem: "" };
  var active = "main";
  var phase = "answering"; // answering | right | wrong | done
  var answers = [];
  var startedAt = Date.now();
  var shownAt = startedAt;
  var timer = null;

  // --- sounds: short tones made in the browser, no files -------------------

  var soundOn = true;
  try { soundOn = window.localStorage.getItem("zigrt.sound") !== "off"; } catch (e) { /* storage may be blocked */ }
  var ctx = null;
  function audio() {
    if (!ctx) {
      var AC = window.AudioContext || window.webkitAudioContext;
      if (!AC) return null;
      ctx = new AC();
    }
    if (ctx.state === "suspended") ctx.resume();
    return ctx;
  }
  function tone(c, freq, start, duration, type, volume) {
    var t = c.currentTime + start;
    var osc = c.createOscillator();
    var gain = c.createGain();
    osc.type = type || "sine";
    osc.frequency.setValueAtTime(freq, t);
    gain.gain.setValueAtTime(0.0001, t);
    gain.gain.exponentialRampToValueAtTime(volume || 0.18, t + 0.015);
    gain.gain.exponentialRampToValueAtTime(0.0001, t + duration);
    osc.connect(gain).connect(c.destination);
    osc.start(t);
    osc.stop(t + duration + 0.05);
  }
  var SOUNDS = {
    right: function (c) { tone(c, 659.25, 0, 0.15); tone(c, 987.77, 0.08, 0.3); },
    wrong: function (c) { tone(c, 392, 0, 0.2, "triangle", 0.22); tone(c, 311.13, 0.16, 0.35, "triangle", 0.22); },
    finish: function (c) { [523.25, 659.25, 783.99].forEach(function (f, i) { tone(c, f, i * 0.12, i === 2 ? 0.6 : 0.2); }); }
  };
  function play(name) {
    if (!soundOn) return;
    var c = audio();
    if (c) SOUNDS[name](c);
  }
  function drawSound() {
    soundBtn.setAttribute("aria-pressed", soundOn ? "true" : "false");
    soundBtn.setAttribute("aria-label", soundOn ? "Sound is on. Turn sound off" : "Sound is off. Turn sound on");
  }
  soundBtn.addEventListener("click", function () {
    soundOn = !soundOn;
    try { window.localStorage.setItem("zigrt.sound", soundOn ? "on" : "off"); } catch (e) { /* ignore */ }
    drawSound();
    if (soundOn) audio();
  });

  // --- drawing ---------------------------------------------------------------

  function current() { return problems[index]; }
  function hasRem(p) { return p.hasAttribute("data-rem"); }

  function drawSlots() {
    var p = current();
    Array.prototype.forEach.call(p.querySelectorAll("[data-slot]"), function (el) {
      var name = el.getAttribute("data-slot");
      el.textContent = input[name];
      el.classList.toggle("active", phase === "answering" && name === active);
    });
    switchKey.textContent = hasRem(p) ? (active === "main" ? "R" : "Back") : "Clear";
    switchKey.setAttribute("aria-label", hasRem(p) ? (active === "main" ? "Go to the remainder box" : "Go back to the answer box") : "Clear");
  }

  function show() {
    problems.forEach(function (p, i) { p.hidden = i !== index; p.classList.remove("right", "wrong"); });
    var p = current();
    // Long problems get smaller type so they fit a phone.
    p.classList.toggle("long", p.textContent.replace(/\s/g, "").length + (hasRem(p) ? 4 : 0) > 9);
    input = { main: "", rem: "" };
    active = "main";
    phase = "answering";
    shownAt = Date.now();
    feedback.className = "feedback";
    feedback.textContent = "";
    checkBtn.textContent = "Check";
    countEl.textContent = (index + 1) + " of " + problems.length;
    progress.value = index;
    setKeys(true);
    drawSlots();
  }

  function setKeys(on) {
    keys.forEach(function (k) { k.disabled = !on; });
  }

  // --- answering -------------------------------------------------------------

  function type(d) {
    if (phase !== "answering" || input[active].length >= MAX_DIGITS) return;
    input[active] = input[active] === "0" ? d : input[active] + d;
    drawSlots();
  }
  function del() {
    if (phase !== "answering") return;
    if (input[active] === "" && active === "rem") active = "main";
    else input[active] = input[active].slice(0, -1);
    drawSlots();
  }
  function switchOrClear() {
    if (phase !== "answering") return;
    if (hasRem(current())) active = active === "main" ? "rem" : "main";
    else input.main = "";
    drawSlots();
  }

  function isRight(p) {
    if (input.main === "" || Number(input.main) !== Number(p.getAttribute("data-answer"))) return false;
    if (!hasRem(p)) return true;
    return Number(input.rem || 0) === Number(p.getAttribute("data-rem"));
  }

  function submit() {
    if (phase === "wrong") return next();
    if (phase !== "answering") return;
    var p = current();
    if (input.main === "") return; // nothing typed yet
    if (hasRem(p) && active === "main" && input.rem === "") { // lead on to the remainder
      active = "rem";
      drawSlots();
      return;
    }
    answers.push({ main: input.main, rem: input.rem, ms: Date.now() - shownAt });
    setKeys(false);
    if (isRight(p)) {
      phase = "right";
      play("right");
      p.classList.add("right");
      feedback.className = "feedback good";
      feedback.textContent = "Yes!";
      drawSlots();
      timer = window.setTimeout(next, RIGHT_PAUSE_MS);
    } else {
      phase = "wrong";
      play("wrong");
      p.classList.add("wrong");
      feedback.className = "feedback notyet";
      feedback.textContent = "";
      var line = document.createElement("span");
      line.textContent = "Not quite. The answer is " + p.getAttribute("data-text") + ".";
      var eq = document.createElement("b");
      eq.textContent = p.getAttribute("data-eq");
      feedback.appendChild(line);
      feedback.appendChild(eq);
      checkBtn.textContent = "Next";
      drawSlots();
    }
  }

  function next() {
    window.clearTimeout(timer);
    if (phase === "done") return;
    if (index + 1 >= problems.length) return end(true);
    index++;
    show();
  }

  // end posts what was answered. Stopping early is fine: the rest count as
  // not answered.
  function end(finished) {
    if (phase === "done") return;
    phase = "done";
    window.clearTimeout(timer);
    if (finished) play("finish");
    finish.elements.answers.value = JSON.stringify(answers);
    finish.elements.duration_ms.value = String(Date.now() - startedAt);
    progress.value = problems.length;
    window.setTimeout(function () { finish.submit(); }, finished ? 350 : 0);
  }

  // --- wiring ----------------------------------------------------------------

  app.addEventListener("click", function (e) {
    var k = e.target.closest("[data-key]");
    if (k) {
      var key = k.getAttribute("data-key");
      if (key === "del") del();
      else if (key === "switch") switchOrClear();
      else type(key);
      return;
    }
    if (e.target.closest("[data-check]")) submit();
    else if (e.target.closest("[data-stop]")) end(false);
    else {
      var slot = e.target.closest("[data-slot]");
      if (slot && phase === "answering") { active = slot.getAttribute("data-slot"); drawSlots(); }
    }
  });

  document.addEventListener("keydown", function (e) {
    if (e.ctrlKey || e.metaKey || e.altKey) return;
    if (e.key >= "0" && e.key <= "9") { type(e.key); e.preventDefault(); }
    else if (e.key === "Backspace") { del(); e.preventDefault(); }
    else if (e.key === "Enter") {
      // Let Enter press a focused button itself, so it isn't counted twice.
      if (document.activeElement && document.activeElement.tagName === "BUTTON") return;
      submit();
      e.preventDefault();
    } else if ((e.key === "r" || e.key === "R") && hasRem(current())) { switchOrClear(); e.preventDefault(); }
  });

  if (!problems.length) return;
  app.hidden = false;
  drawSound();
  show();
})();
