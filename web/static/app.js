// enghi - a small amount of vanilla JS: keyboard handling and the focus
// channel, nothing more.

// ---------------------------------------------------------------- messages
//
// **Never write messages inline in the JS.** The server puts the messages for
// the current language into data-strings on body; they are read from there.

var S = (function () {
  try {
    return JSON.parse(document.body.getAttribute("data-strings") || "{}");
  } catch (e) { return {}; }
})();

function t(key, arg) {
  var s = S[key] || key;
  return arg === undefined ? s : s.replace(/%[sd]/, arg);
}

// ---------------------------------------------------------------- focus channel
//
// DESIGN 4.3: search and select in Emacs, and the browser left open on another
// display follows.
// **Automatic reconnection with exponential backoff is mandatory** (DESIGN 8-8).
// Without it, a server restart leaves "focus does not arrive for some reason"
// for the rest of the day.

(function () {
  var backoff = 500;           // ms
  var BACKOFF_MAX = 30000;
  var ws = null;
  var timer = null;

  function url() {
    var proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    return proto + '//' + window.location.host + '/api/events';
  }

  function connect() {
    timer = null;
    if (ws) return;            // never open a second connection
    try {
      ws = new WebSocket(url());
    } catch (e) {
      schedule();
      return;
    }

    ws.onopen = function () {
      backoff = 500;           // reset the backoff once connected
    };

    ws.onmessage = function (ev) {
      var msg;
      try { msg = JSON.parse(ev.data); } catch (e) { return; }
      if (msg.type === 'navigate' && msg.path) {
        if (window.location.pathname + window.location.search !== msg.path) {
          window.location.assign(msg.path);
        }
      }
      // Reload when the page on screen was updated elsewhere; never while
      // editing
      if (msg.type === 'updated' && msg.slug) {
        // **pathname is percent-encoded while the slug is raw.** For a Japanese
        // slug a direct comparison always fails, so the decoded form is
        // compared as well.
        var here = window.location.pathname;
        var hereDecoded = here;
        try { hereDecoded = decodeURIComponent(here); } catch (e) { /* malformed URL */ }
        if ((here === '/wiki/' + msg.slug || hereDecoded === '/wiki/' + msg.slug) &&
            !document.querySelector('textarea')) {
          // When reading along while editing in Emacs, jumping back to the top
          // on every reload makes it useless. The scroll position is carried
          // over (restored by restoreScroll below).
          try {
            sessionStorage.setItem('enghi:scroll:' + here,
                                   JSON.stringify({ y: window.scrollY, t: Date.now() }));
          } catch (e) { /* give up in private mode and the like */ }
          window.location.reload();
        }
      }
    };

    // **Automatic reconnection with exponential backoff is mandatory**
    // (DESIGN 8-8). Without it, a server restart leaves "focus does not arrive
    // for some reason" for the rest of the day.
    ws.onclose = function () { ws = null; schedule(); };
    ws.onerror = function () { if (ws) { ws.close(); } };
  }

  function disconnect() {
    if (ws) {
      ws.onclose = null;       // a deliberate close must not schedule a retry
      ws.close();
      ws = null;
    }
  }

  function schedule() {
    if (timer) return;         // a retry is already pending
    timer = setTimeout(connect, backoff);
    backoff = Math.min(backoff * 2, BACKOFF_MAX);
  }

  // Close when leaving the page, so a page kept in the bfcache does not hold
  // the connection.
  window.addEventListener('pagehide', function () {
    if (timer) { clearTimeout(timer); timer = null; }
    disconnect();
  });

  // Re-establish it when restored from the bfcache.
  window.addEventListener('pageshow', function () {
    backoff = 500;
    if (!ws && !timer) connect();
  });

  connect();
})();

// ------------------------------------------------- carrying the scroll position
//
// Only a reload triggered by the updated event above returns to the previous
// scroll position. So that ordinary navigation and refreshes are not caught up
// in it, the marker is cleared once used and stale ones are discarded.

(function () {
  var key = 'enghi:scroll:' + window.location.pathname;
  var raw;
  try {
    raw = sessionStorage.getItem(key);
    if (raw) { sessionStorage.removeItem(key); }
  } catch (e) { return; }
  if (!raw) return;
  var saved;
  try { saved = JSON.parse(raw); } catch (e) { return; }
  if (!saved || Date.now() - saved.t > 10000) return;   // ignore markers older than 10s
  window.scrollTo(0, saved.y);
})();

// ---------------------------------------------------------------- keyboard
//
// DESIGN 6: the keyboard is a first-class way to drive this.
//   /            ... focus the search box
//   g d/w/i/n/p  ... dashboard / wiki / inbox / next / projects
//   e            ... edit the article on screen
//   j / k        ... move within a list; Enter opens

(function () {
  var pendingG = false;
  var gTimer = null;

  function inField(el) {
    if (!el) return false;
    var t = el.tagName;
    return t === 'INPUT' || t === 'TEXTAREA' || t === 'SELECT' || el.isContentEditable;
  }

  function rows() {
    return Array.prototype.slice.call(document.querySelectorAll('ul.rows > li'));
  }

  function cursorIndex(list) {
    for (var i = 0; i < list.length; i++) {
      if (list[i].classList.contains('cur')) return i;
    }
    return -1;
  }

  function moveCursor(delta) {
    var list = rows();
    if (!list.length) return;
    var i = cursorIndex(list);
    if (i >= 0) list[i].classList.remove('cur');
    var next = i < 0 ? (delta > 0 ? 0 : list.length - 1) : i + delta;
    if (next < 0) next = 0;
    if (next >= list.length) next = list.length - 1;
    list[next].classList.add('cur');
    list[next].scrollIntoView({ block: 'nearest' });
  }

  // ---- act on the task under the cursor with a single key
  //
  // The server already has an endpoint for every state change, so this just
  // builds a form and submits it.
  // **A form submission, not fetch.** It carries Origin and Sec-Fetch-Site and
  // goes through the same path as the screens (formAllowed in the secure
  // middleware).
  //
  // `k' already moves the cursor up, so "skip this one" - `k' in agenda - has
  // been moved to `S'.
  function post(path, fields) {
    var form = document.createElement('form');
    form.method = 'post';
    form.action = path;
    fields = fields || {};
    fields.return_to = window.location.pathname + window.location.search;
    Object.keys(fields).forEach(function (name) {
      var input = document.createElement('input');
      input.type = 'hidden';
      input.name = name;
      input.value = fields[name];
      form.appendChild(input);
    });
    document.body.appendChild(form);
    form.submit();
  }

  var STATES = { n: 'next', l: 'later', m: 'someday' };
  var PATHS = { d: 'complete', S: 'skip', f: 'file' };

  function cursorTask() {
    var list = rows();
    var i = cursorIndex(list);
    if (i < 0) return null;
    var id = list[i].getAttribute('data-task-id');
    if (!id) return null;                       // a non-task row, such as in an article list
    var link = list[i].querySelector('a[href]');
    return { id: id, title: link ? link.textContent.trim() : '' };
  }

  function taskKey(key) {
    var task = cursorTask();
    if (!task) return false;
    var base = '/ui/tasks/' + task.id;

    if (STATES[key]) { post(base, { state: STATES[key] }); return true; }

    if (key === 'w') {
      var who = window.prompt(t('keys.ask_waiting'));
      if (who) post(base, { state: 'waiting', waiting_for: who });
      return true;
    }
    if (key === 's') {
      var on = window.prompt(t('keys.ask_scheduled'));
      if (on) post(base, { state: 'scheduled', scheduled_on: on });
      return true;
    }
    if (key === 't') {
      var title = window.prompt(t('keys.ask_title'), task.title);
      if (title) post(base, { title: title });
      return true;
    }
    if (key === 'x') {
      // Dropping is the one that cannot be undone, so confirm it
      if (window.confirm(t('keys.confirm_drop', task.title))) post(base + '/delete');
      return true;
    }
    if (PATHS[key]) { post(base + '/' + PATHS[key]); return true; }
    return false;
  }

  function openCursor() {
    var list = rows();
    var i = cursorIndex(list);
    if (i < 0) return false;
    var a = list[i].querySelector('a[href]');
    if (!a) return false;
    window.location.assign(a.getAttribute('href'));
    return true;
  }

  document.addEventListener('keydown', function (ev) {
    if (ev.metaKey || ev.ctrlKey || ev.altKey) return;

    if (inField(document.activeElement)) {
      if (ev.key === 'Escape') { document.activeElement.blur(); }
      return;
    }

    if (pendingG) {
      pendingG = false;
      clearTimeout(gTimer);
      var dest = { d: '/', w: '/wiki', i: '/gtd/inbox', n: '/gtd/next', p: '/gtd/projects' }[ev.key];
      if (dest) { ev.preventDefault(); window.location.assign(dest); return; }
    }

    switch (ev.key) {
      case '/':
        ev.preventDefault();
        var box = document.getElementById('q');
        if (box) { box.focus(); box.select(); }
        break;
      case 'g':
        pendingG = true;
        gTimer = setTimeout(function () { pendingG = false; }, 900);
        break;
      case 'c':
        ev.preventDefault();
        openCapture();
        break;
      case 'e':
        var edit = document.querySelector('a[data-key="edit"]');
        if (edit) { ev.preventDefault(); window.location.assign(edit.getAttribute('href')); }
        break;
      case 'j':
        ev.preventDefault(); moveCursor(1); break;
      case 'k':
        ev.preventDefault(); moveCursor(-1); break;
      default:
        if (taskKey(ev.key)) ev.preventDefault();
        break;
      case 'Enter':
        if (openCursor()) ev.preventDefault();
        break;
      case 'Escape':
        var sg = document.getElementById('suggest');
        if (sg) sg.innerHTML = '';
        break;
    }
  });
})();

// ---------------------------------------------------------------- theme switch
//
// Cycles auto (follow the OS) -> light -> dark -> auto.
// The choice lives in localStorage. On auto the attribute is removed and CSS
// prefers-color-scheme takes over.

(function () {
  var KEY = "enghi-theme";
  var ORDER = ["auto", "light", "dark"];
  var LABEL = { auto: "theme.auto", light: "theme.light", dark: "theme.dark" };

  function current() {
    try {
      var t = localStorage.getItem(KEY);
      return (t === "light" || t === "dark") ? t : "auto";
    } catch (e) { return "auto"; }
  }

  function apply(theme) {
    if (theme === "auto") {
      delete document.documentElement.dataset.theme;
    } else {
      document.documentElement.dataset.theme = theme;
    }
    try {
      if (theme === "auto") localStorage.removeItem(KEY);
      else localStorage.setItem(KEY, theme);
    } catch (e) {}
    var btn = document.getElementById("theme-toggle");
    if (btn) btn.title = t("theme.toggle") + ": " + t(LABEL[theme]);
  }

  var btn = document.getElementById("theme-toggle");
  if (btn) {
    apply(current());
    btn.addEventListener("click", function () {
      apply(ORDER[(ORDER.indexOf(current()) + 1) % ORDER.length]);
    });
  }
})();

// ---------------------------------------------------------------- quick capture
//
// DESIGN 6: c opens a modal that adds one line to the inbox from anywhere.
// Getting something out of your head must start with a single keystroke, on any
// screen.

function openCapture() {
  if (document.getElementById('capture-modal')) return;

  var overlay = document.createElement('div');
  overlay.id = 'capture-modal';
  overlay.className = 'modal-overlay';

  var box = document.createElement('div');
  box.className = 'modal';

  var label = document.createElement('div');
  label.className = 'modal-label';
  label.textContent = t("capture.title");

  var input = document.createElement('input');
  input.type = 'text';
  input.placeholder = t("gtd.capture_short");

  var hint = document.createElement('div');
  hint.className = 'modal-hint';
  hint.textContent = t("capture.hint");

  box.appendChild(label);
  box.appendChild(input);
  box.appendChild(hint);
  overlay.appendChild(box);
  document.body.appendChild(overlay);
  input.focus();

  function close() { overlay.remove(); }

  overlay.addEventListener('click', function (e) { if (e.target === overlay) close(); });

  input.addEventListener('keydown', function (e) {
    e.stopPropagation();
    if (e.key === 'Escape') { close(); return; }
    if (e.key !== 'Enter') return;
    var title = input.value.trim();
    if (!title) { close(); return; }
    fetch('/api/tasks', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: title }),
    }).then(function (r) {
      if (!r.ok) throw new Error('capture failed');
      label.textContent = t("capture.added");
      input.value = '';
      setTimeout(close, 400);
    }).catch(function () {
      label.textContent = t("capture.failed");
    });
  });
}

// ---------------------------------------------------------------- pasting images
//
// Pasting or dropping an image into the editing textarea uploads it there and
// then and inserts the Markdown at the cursor.

(function () {
  var ta = document.querySelector('textarea[name=body]');
  if (!ta) return;

  function insertAtCursor(text) {
    var start = ta.selectionStart, end = ta.selectionEnd;
    ta.value = ta.value.slice(0, start) + text + ta.value.slice(end);
    ta.selectionStart = ta.selectionEnd = start + text.length;
    ta.dispatchEvent(new Event('input', { bubbles: true }));
  }

  function upload(file, placeholder) {
    return fetch('/api/files?name=' + encodeURIComponent(file.name || ''), {
      method: 'POST',
      headers: { 'Content-Type': file.type },
      body: file,
    }).then(function (r) {
      if (!r.ok) return r.json().then(function (e) { throw new Error(e.message || r.status); });
      return r.json();
    }).then(function (res) {
      // Replace the placeholder with the real link
      ta.value = ta.value.replace(placeholder, res.markdown);
      ta.dispatchEvent(new Event('input', { bubbles: true }));
    }).catch(function (err) {
      ta.value = ta.value.replace(placeholder, '<!-- ' + t("capture.upload_failed", err.message) + ' -->');
    });
  }

  function handle(fileList) {
    var files = Array.prototype.slice.call(fileList).filter(function (f) {
      return f && (f.type.indexOf('image/') === 0 || f.type === 'application/pdf');
    });
    if (!files.length) return false;
    files.forEach(function (file, i) {
      // Until the response arrives, show where it will land
      var placeholder = '![' + t("capture.uploading") + Date.now() + '-' + i + ']()';
      insertAtCursor(placeholder + '\n');
      upload(file, placeholder);
    });
    return true;
  }

  ta.addEventListener('paste', function (ev) {
    if (ev.clipboardData && handle(ev.clipboardData.files)) ev.preventDefault();
  });

  ta.addEventListener('dragover', function (ev) {
    if (ev.dataTransfer && ev.dataTransfer.types.indexOf('Files') >= 0) {
      ev.preventDefault();
      ta.classList.add('dropping');
    }
  });
  ta.addEventListener('dragleave', function () { ta.classList.remove('dropping'); });
  ta.addEventListener('drop', function (ev) {
    ta.classList.remove('dropping');
    if (ev.dataTransfer && handle(ev.dataTransfer.files)) ev.preventDefault();
  });
})();

// ---------------------------------------------------------------- edit preview
(function () {
  var btn = document.getElementById('preview-toggle');
  if (!btn) return;
  btn.addEventListener('click', function () {
    var pane = document.getElementById('preview');
    var ta = document.querySelector('textarea[name=body]');
    if (!pane || !ta) return;
    if (pane.style.display === 'block') { pane.style.display = 'none'; return; }
    fetch('/ui/preview', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: 'body=' + encodeURIComponent(ta.value)
    }).then(function (r) { return r.text(); })
      .then(function (html) { pane.innerHTML = html; pane.style.display = 'block'; });
  });
})();

// ---------------------------------------------------------------- [[...]] completion
//
// Typing `[[` while editing shows titles and aliases at the caret, so a link
// can be made without remembering the exact title. The main point is to stop a
// typo from silently becoming an unresolved link, so **every candidate offered
// is guaranteed to resolve** (the server queries page_titles alone).
//
// With no candidates, the only row offered inserts what was typed as is. An
// unresolved link is a legitimate way to say "an article I am about to write",
// so it is never blocked (DESIGN 2.1).

(function () {
  var ta = document.querySelector('textarea[name=body]');
  if (!ta) return;

  var box = null;     // the container for the candidates
  var items = [];     // candidates; the last one may be "insert as typed"
  var sel = 0;        // which one is selected
  var open = 0;       // where `[[` starts in the body
  var timer = null;
  var seq = 0;        // guards against out-of-order responses when typing fast

  // Where the caret is on screen. A textarea has no API for that, so the same
  // text is poured into a hidden element with the same styling and measured.
  function caretXY(pos) {
    var cs = getComputedStyle(ta);
    var m = document.createElement('div');
    ['fontFamily', 'fontSize', 'fontWeight', 'fontStyle', 'letterSpacing', 'lineHeight',
     'textTransform', 'wordSpacing', 'paddingTop', 'paddingRight', 'paddingBottom',
     'paddingLeft', 'borderTopWidth', 'borderRightWidth', 'borderBottomWidth',
     'borderLeftWidth', 'boxSizing', 'tabSize'].forEach(function (k) { m.style[k] = cs[k]; });
    m.style.position = 'absolute';
    m.style.visibility = 'hidden';
    m.style.whiteSpace = 'pre-wrap';
    m.style.wordWrap = 'break-word';
    m.style.width = ta.clientWidth + 'px';
    m.textContent = ta.value.slice(0, pos);
    var mark = document.createElement('span');
    mark.textContent = '​';
    m.appendChild(mark);
    document.body.appendChild(m);
    var r = ta.getBoundingClientRect();
    var xy = {
      x: r.left + window.scrollX + mark.offsetLeft - ta.scrollLeft,
      y: r.top + window.scrollY + mark.offsetTop - ta.scrollTop,
      h: parseFloat(cs.lineHeight) || 20,
    };
    document.body.removeChild(m);
    return xy;
  }

  // Find the `[[` before the caret. Completion stops at a closing bracket, a
  // newline or a `|` (after a `|` comes the label, not the title).
  function context() {
    var pos = ta.selectionStart;
    if (pos !== ta.selectionEnd) return null;
    var head = ta.value.slice(0, pos);
    var i = head.lastIndexOf('[[');
    if (i < 0) return null;
    var q = head.slice(i + 2);
    if (/[\[\]|\n]/.test(q)) return null;
    return { start: i, q: q };
  }

  function close() {
    if (box) { box.remove(); box = null; }
    items = [];
  }

  function render() {
    if (!box) {
      box = document.createElement('div');
      box.className = 'wl-suggest';
      document.body.appendChild(box);
    }
    box.innerHTML = '';
    items.forEach(function (it, i) {
      var row = document.createElement('div');
      row.className = 'wl-item' + (i === sel ? ' on' : '');
      var name = document.createElement('span');
      name.className = 'wl-title';
      name.textContent = it.create ? t('wikilink.create', it.title) : it.title;
      row.appendChild(name);
      if (it.is_alias) {
        var note = document.createElement('span');
        note.className = 'wl-note';
        note.textContent = t('wikilink.alias', it.canonical);
        row.appendChild(note);
      }
      row.addEventListener('mousedown', function (ev) { ev.preventDefault(); pick(i); });
      box.appendChild(row);
    });
    var hint = document.createElement('div');
    hint.className = 'wl-hint';
    hint.textContent = t('wikilink.hint');
    box.appendChild(hint);

    var xy = caretXY(ta.selectionStart);
    box.style.left = Math.min(xy.x, window.scrollX + document.documentElement.clientWidth - box.offsetWidth - 8) + 'px';
    box.style.top = (xy.y + xy.h) + 'px';
  }

  function pick(i) {
    var it = items[i];
    if (!it) return;
    var ctx = context();
    if (!ctx) { close(); return; }
    var insert = '[[' + it.title + ']]';
    var pos = ta.selectionStart;
    ta.value = ta.value.slice(0, ctx.start) + insert + ta.value.slice(pos);
    ta.selectionStart = ta.selectionEnd = ctx.start + insert.length;
    close();
    ta.focus();
    ta.dispatchEvent(new Event('input', { bubbles: true }));
  }

  function refresh() {
    var ctx = context();
    if (!ctx) { close(); return; }
    open = ctx.start;
    var my = ++seq;
    fetch('/api/titles?limit=8&q=' + encodeURIComponent(ctx.q))
      .then(function (r) { return r.json(); })
      .then(function (res) {
        if (my !== seq) return;          // drop a response that was overtaken
        var now = context();
        if (!now || now.start !== open) { close(); return; }
        items = (res.titles || []).map(function (v) { return v; });
        // With an exact match already present, "insert as typed" is dropped:
        // it would list the same row twice
        var exact = items.some(function (v) {
          return v.title.toLowerCase() === now.q.trim().toLowerCase();
        });
        if (now.q.trim() && !exact) items.push({ title: now.q.trim(), create: true });
        if (!items.length) { close(); return; }
        sel = 0;
        render();
      })
      .catch(close);
  }

  function schedule() {
    clearTimeout(timer);
    timer = setTimeout(refresh, 100);   // same interval as the search box
  }

  ta.addEventListener('input', schedule);
  ta.addEventListener('click', schedule);
  ta.addEventListener('blur', function () { setTimeout(close, 100); });

  ta.addEventListener('keydown', function (ev) {
    if (!box || !items.length) return;
    if (ev.key === 'ArrowDown' || (ev.ctrlKey && ev.key === 'n')) {
      sel = (sel + 1) % items.length; render(); ev.preventDefault();
    } else if (ev.key === 'ArrowUp' || (ev.ctrlKey && ev.key === 'p')) {
      sel = (sel - 1 + items.length) % items.length; render(); ev.preventDefault();
    } else if (ev.key === 'Enter' || ev.key === 'Tab') {
      pick(sel); ev.preventDefault();
    } else if (ev.key === 'Escape') {
      close(); ev.preventDefault(); ev.stopPropagation();   // do not pass it to quick capture
    }
  });
})();
