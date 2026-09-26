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

// Each %s / %d takes the next argument, in order
function t(key) {
  var args = Array.prototype.slice.call(arguments, 1), i = 0;
  return (S[key] || key).replace(/%[sd]/g, function (m) {
    return i < args.length ? args[i++] : m;
  });
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

// ---------------------------------------------------------------- repeat picker
//
// Used by the Scheduled step of the move modal and by Clarify. It covers the
// subset of ParseRecurrence (internal/gtd/recurrence.go) that fits a picker. Any other
// rule, such as ++1w, is shown as Custom and sent back unchanged. The server
// stays the validator.
var WEEKDAYS = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat'];   // Date.getDay() order
var UNITS = [['d', 'days'], ['w', 'weeks'], ['m', 'months'], ['y', 'years']];

function parseRule(raw) {
  var s = (raw || '').trim().toLowerCase(), m;
  if (!s) return { kind: '' };
  if ((m = /^weekly:(.+)$/.exec(s))) {
    var days = m[1].split(',').map(function (d) { return d.trim(); });
    if (days.every(function (d) { return WEEKDAYS.indexOf(d) >= 0; })) {
      return { kind: 'weekly', days: days };
    }
  } else if ((m = /^monthly:\s*(last|\d{1,2})\s*$/.exec(s))) {
    if (m[1] === 'last' || (+m[1] >= 1 && +m[1] <= 31)) {
      return { kind: 'monthly', dom: m[1] === 'last' ? 'last' : String(+m[1]) };
    }
  } else if ((m = /^yearly:(\d{1,2})-(\d{1,2})$/.exec(s))) {
    if (+m[1] >= 1 && +m[1] <= 12 && +m[2] >= 1 && +m[2] <= 31) {
      return { kind: 'yearly', mon: String(+m[1]), day: String(+m[2]) };
    }
  } else if ((m = /^(\.?\+)(\d+)([dwmy])$/.exec(s))) {   // ++ falls through to Custom
    if (+m[2] >= 1) return { kind: 'interval', n: +m[2], unit: m[3], fromDone: m[1] === '.+' };
  }
  return { kind: 'custom', raw: raw };
}

function pad2(n) { return (+n < 10 ? '0' : '') + (+n); }

function el(tag, cls, text) {
  var e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function numberSelect(from, to, extra) {
  var s = el('select');
  for (var i = from; i <= to; i++) s.appendChild(new Option(String(i), String(i)));
  if (extra) s.appendChild(extra);
  return s;
}

function picked(type, value) {
  var i = el('input');
  i.type = type; i.value = value;
  return i;
}

// The "Repeat" group that goes under a date field. Only the controls of the
// chosen kind are shown; the rest are disabled so the browser does not
// validate them. The weekday, day of month and month-day follow the date field
// until they are set by hand (or prefilled from the rule), so picking a kind
// right after the date needs no more typing.
// Returns the fieldset and a function giving the rule and its end date.
function repeatPicker(date, recurrence, endsOn) {
  var rule = parseRule(recurrence);
  var fs = el('fieldset', 'repeat');
  fs.appendChild(el('legend', '', t('repeat')));

  var kinds = ['', 'interval', 'weekly', 'monthly', 'yearly'];
  if (rule.kind === 'custom') kinds.push('custom');
  var kind = el('select');
  kinds.forEach(function (k) {
    kind.appendChild(new Option(t('repeat.' + (k || 'none')), k));
  });
  kind.value = rule.kind;
  fs.appendChild(kind);

  var groups = {};
  function group(k) {
    var g = el('div', 'repeat-group');
    fs.appendChild(g);
    groups[k] = g;
    return g;
  }

  // Every N days / weeks / months / years, counted from either date
  var g = group('interval');
  var row = el('div', 'repeat-row');
  row.appendChild(el('span', '', t('repeat.every')));
  var n = picked('number', String(rule.n || 1));
  n.required = true;
  n.min = '1';
  var unit = el('select');
  UNITS.forEach(function (u) { unit.appendChild(new Option(t('repeat.' + u[1]), u[0])); });
  unit.value = rule.unit || 'w';
  row.appendChild(n);
  row.appendChild(unit);
  g.appendChild(row);
  var from = el('select');
  from.appendChild(new Option(t('repeat.from_scheduled'), '+'));
  from.appendChild(new Option(t('repeat.from_done'), '.+'));
  from.value = rule.fromDone ? '.+' : '+';
  g.appendChild(from);

  // On weekdays
  g = group('weekly');
  var days = el('div', 'repeat-days');
  var checks = WEEKDAYS.map(function (d) {
    var label = el('label');
    var c = picked('checkbox', d);
    c.checked = !!rule.days && rule.days.indexOf(d) >= 0;
    label.appendChild(c);
    label.appendChild(el('span', '', t('repeat.wd.' + d)));
    days.appendChild(label);
    return c;
  });
  g.appendChild(days);

  // Monthly on day
  g = group('monthly');
  var dom = numberSelect(1, 31, new Option(t('repeat.last_day'), 'last'));
  g.appendChild(dom);

  // Yearly on MM-DD
  g = group('yearly');
  row = el('div', 'repeat-row');
  var ymon = numberSelect(1, 12), yday = numberSelect(1, 31);
  row.appendChild(ymon);
  row.appendChild(el('span', '', '-'));
  row.appendChild(yday);
  g.appendChild(row);

  g = group('custom');
  g.appendChild(el('code', '', rule.raw || ''));

  var ends = picked('date', endsOn || '');
  var endsLabel = el('label', 'repeat-field');
  endsLabel.appendChild(el('span', '', t('repeat.ends_on')));
  endsLabel.appendChild(ends);
  fs.appendChild(endsLabel);

  var byHand = {
    weekly: rule.kind === 'weekly', monthly: rule.kind === 'monthly',
    yearly: rule.kind === 'yearly',
  };
  if (rule.kind === 'monthly') dom.value = rule.dom;
  if (rule.kind === 'yearly') { ymon.value = rule.mon; yday.value = rule.day; }
  ['weekly', 'monthly', 'yearly'].forEach(function (k) {
    groups[k].addEventListener('change', function () { byHand[k] = true; check(); });
  });

  function followDate() {
    var m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date.value);
    var d = m ? new Date(+m[1], m[2] - 1, +m[3]) : new Date();
    if (!byHand.weekly) checks.forEach(function (c, i) { c.checked = i === d.getDay(); });
    if (!byHand.monthly) dom.value = String(d.getDate());
    if (!byHand.yearly) { ymon.value = String(d.getMonth() + 1); yday.value = String(d.getDate()); }
    check();
  }

  // A weekly rule needs at least one day
  function check() {
    var none = !checks.some(function (c) { return c.checked; });
    checks[0].setCustomValidity(kind.value === 'weekly' && none ? t('repeat.pick_day') : '');
  }

  function show() {
    Object.keys(groups).forEach(function (k) {
      var off = kind.value !== k;
      groups[k].hidden = off;
      Array.prototype.forEach.call(groups[k].querySelectorAll('input, select'),
        function (x) { x.disabled = off; });
    });
    endsLabel.hidden = ends.disabled = kind.value === '';
    check();
  }

  kind.addEventListener('change', show);
  date.addEventListener('input', followDate);
  followDate();
  show();

  return { node: fs, value: function () {
    var r = '';
    switch (kind.value) {
      case 'interval': r = from.value + Math.max(1, parseInt(n.value, 10) || 1) + unit.value; break;
      case 'weekly':
        r = 'weekly:' + checks.filter(function (c) { return c.checked; })
          .map(function (c) { return c.value; }).join(',');
        break;
      case 'monthly': r = 'monthly:' + dom.value; break;
      case 'yearly': r = 'yearly:' + pad2(ymon.value) + '-' + pad2(yday.value); break;
      case 'custom': r = rule.raw; break;
    }
    // "None" clears the rule; the picker was prefilled, so this is deliberate
    return { recurrence: r, recurrence_ends_on: r ? ends.value : '' };
  } };
}

// On Clarify the text field for the rule stays for no-JS. With JS it gives way
// to the picker, and the rule is written back into it on submit.
(function () {
  var raw = document.getElementById('recurrence');
  var ends = document.getElementById('recurrence_ends_on');
  var date = document.getElementById('scheduled_on');
  if (!raw || !ends || !date) return;
  var picker = repeatPicker(date, raw.value, ends.value);
  var label = document.querySelector('label[for=recurrence]');
  if (label) label.remove();
  raw.type = 'hidden';
  raw.parentNode.insertBefore(picker.node, raw);
  raw.form.addEventListener('submit', function () {
    var v = picker.value();
    raw.value = v.recurrence;
    ends.value = v.recurrence_ends_on;
  });
})();

// ---------------------------------------------------------------- keyboard
//
// DESIGN 6: the keyboard is a first-class way to drive this.
//   /            ... focus the search box
//   g d/w/i/n/p  ... dashboard / wiki / inbox / next / projects
//   g l          ... the day page (today's work record)
//   [ / ] / t    ... on the day page: previous / next day / today
//   e            ... edit the article on screen
//   j / k        ... move within a list; Enter opens
//   u            ... undo the last move, while its toast is up
//   p            ... start or pause work on the task under the cursor

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

  var STATES = { i: 'inbox', n: 'next', m: 'someday' };
  var PATHS = { d: 'complete', S: 'skip', f: 'file' };
  // These need a value or a confirmation first, so they open the second step
  // of the move modal instead of posting at once.
  var ASKS = { l: 'later', w: 'waiting', s: 'scheduled', x: 'dropped' };

  function taskOf(li) {
    if (!li || !li.getAttribute('data-task-id')) return null;
    var d = li.dataset;
    var link = li.querySelector('a.task-title');
    return {
      id: d.taskId, state: d.state || '',
      title: d.title || (link ? link.textContent.trim() : ''),
      projectId: d.projectId || '', projectTitle: d.projectTitle || '',
      contextId: d.contextId || '',
      waitingFor: d.waitingFor || '', scheduledOn: d.scheduledOn || '',
      recurrence: d.recurrence || '', recurrenceEndsOn: d.recurrenceEndsOn || '',
      delegatedAt: d.delegatedAt || '', version: d.version || '',
      working: d.working === 'true',
    };
  }

  // ---- undoing a move
  //
  // One level, kept on the client. Just before a move is posted, what the row
  // said about the task goes into sessionStorage; the page the move lands on
  // offers to put it back, with a toast and `u'.
  //
  // **The record lives for exactly one page load.** The next page takes it out
  // of storage at once, and shows the toast only when it is the page the move
  // returned to, within UNDO_ARRIVE_MS, and the row (if still on screen) shows
  // the move took effect. From then on the record is held only by the toast:
  // dismissing it, letting it time out, or going to another page drops it, so
  // a stale undo can never fire on some later page.
  //
  // Done on a recurring task is not offered (completing it added the next
  // instance), nor are filing as reference (it wrote an article) and Skip.
  var UNDO_KEY = 'enghi:undo';
  var UNDO_ARRIVE_MS = 15000;
  var UNDO_SHOW_MS = 12000;
  var UNDOABLE = ['inbox', 'next', 'later', 'waiting', 'scheduled', 'someday', 'done', 'dropped'];

  function rememberMove(task, dest) {
    if (UNDOABLE.indexOf(task.state) < 0 || UNDOABLE.indexOf(dest) < 0) return;
    if (dest === task.state || !task.version) return;
    if (dest === 'done' && task.recurrence) return;
    var rec = {
      id: task.id, title: task.title, dest: dest,
      // The move bumps the version by one; anything more means the task was
      // edited elsewhere since, and the server refuses the undo
      version: String(+task.version + 1),
      prev: {
        state: task.state, scheduled_on: task.scheduledOn, waiting_for: task.waitingFor,
        project_id: task.projectId, context_id: task.contextId,
        recurrence: task.recurrence, recurrence_ends_on: task.recurrenceEndsOn,
      },
      path: window.location.pathname + window.location.search, t: Date.now(),
    };
    // Only when there is one: an empty date would stop the server from dating
    // a return to waiting today
    if (task.delegatedAt) rec.prev.delegated_at = task.delegatedAt;
    try { sessionStorage.setItem(UNDO_KEY, JSON.stringify(rec)); } catch (e) { /* no undo then */ }
  }

  function takeMove() {
    var raw;
    try {
      raw = sessionStorage.getItem(UNDO_KEY);
      if (raw) sessionStorage.removeItem(UNDO_KEY);
    } catch (e) { return null; }
    if (!raw) return null;
    var rec;
    try { rec = JSON.parse(raw); } catch (e) { return null; }
    if (!rec || !rec.prev || Date.now() - rec.t > UNDO_ARRIVE_MS) return null;
    if (rec.path !== window.location.pathname + window.location.search) return null;
    // A move the server turned down leaves the row as it was
    var li = document.querySelector('li[data-task-id="' + rec.id + '"]');
    if (li && (li.dataset.state !== rec.dest || li.dataset.version !== rec.version)) return null;
    return rec;
  }

  var undoToast = null;   // { rec, node, timer } while the toast is up

  function dismissUndo() {
    if (!undoToast) return;
    clearTimeout(undoToast.timer);
    undoToast.node.remove();
    undoToast = null;
  }

  function showUndo(rec) {
    var node = el('div', 'toast');
    node.setAttribute('role', 'status');
    var label = rec.dest === 'dropped' ? 'undo.dropped' : 'move.choice.' + rec.dest;
    var msg = el('span', 'toast-msg', t('undo.moved', rec.title, t(label)));
    var undo = el('button', 'toast-action', t('undo.action'));
    undo.type = 'button';
    undo.addEventListener('click', runUndo);
    var close = el('button', 'toast-close', '×');
    close.type = 'button';
    close.title = t('undo.dismiss');
    close.addEventListener('click', dismissUndo);
    node.appendChild(msg);
    node.appendChild(undo);
    node.appendChild(close);
    document.body.appendChild(node);
    undoToast = { rec: rec, node: node, msg: msg, undo: undo, timer: 0 };
    // Hovering holds it, so it does not vanish under the pointer
    function arm() { undoToast.timer = setTimeout(dismissUndo, UNDO_SHOW_MS); }
    node.addEventListener('mouseenter', function () { if (undoToast) clearTimeout(undoToast.timer); });
    node.addEventListener('mouseleave', function () { if (undoToast) arm(); });
    arm();
  }

  // **fetch, unlike post().** On a conflict the page must stay put and say so
  // in the toast. It is still a same-origin form post to /ui/, so it passes
  // formAllowed as the screens do.
  function runUndo() {
    if (!undoToast || undoToast.busy) return;
    var u = undoToast;
    u.busy = true;
    clearTimeout(u.timer);
    var body = new URLSearchParams();
    Object.keys(u.rec.prev).forEach(function (k) { body.append(k, u.rec.prev[k]); });
    body.append('version', u.rec.version);
    fetch('/ui/tasks/' + u.rec.id, { method: 'POST', body: body, credentials: 'same-origin' })
      .then(function (r) {
        if (r.ok) {
          try {
            sessionStorage.setItem('enghi:scroll:' + window.location.pathname,
                                   JSON.stringify({ y: window.scrollY, t: Date.now() }));
          } catch (e) { /* give up in private mode and the like */ }
          window.location.reload();
          return;
        }
        u.msg.textContent = t(r.status === 409 ? 'undo.conflict' : 'undo.failed');
        u.undo.remove();
        u.node.classList.add('warn');
        u.timer = setTimeout(dismissUndo, UNDO_SHOW_MS);
      })
      .catch(function () {
        u.msg.textContent = t('undo.failed');
        u.undo.remove();
      });
  }

  var arrived = takeMove();
  if (arrived) showUndo(arrived);

  function cursorRow() {
    var list = rows();
    var i = cursorIndex(list);
    return i < 0 ? null : list[i];
  }

  function taskKey(key) {
    var task = taskOf(cursorRow());     // null on a non-task row, such as in an article list
    if (!task) return false;
    var base = '/ui/tasks/' + task.id;

    if (STATES[key]) {
      rememberMove(task, STATES[key]);
      post(base, { state: STATES[key] });
      return true;
    }
    if (ASKS[key]) { openMove(task, ASKS[key]); return true; }
    // Start and pause share one key: whichever applies. It posts like the
    // buttons on Clarify and comes back to this list.
    if (key === 'p') {
      post(base + (task.working ? '/pause' : '/start'));
      return true;
    }
    if (key === 't') {
      var title = window.prompt(t('keys.ask_title'), task.title);
      if (title) post(base, { title: title });
      return true;
    }
    if (PATHS[key]) {
      if (PATHS[key] === 'complete') rememberMove(task, 'done');
      post(base + '/' + PATHS[key]);
      return true;
    }
    return false;
  }

  function openCursor() {
    var li = cursorRow();
    if (!li) return false;
    var task = taskOf(li);
    if (task) { openMove(task); return true; }
    var a = li.querySelector('a[href]');
    if (!a) return false;
    window.location.assign(a.getAttribute('href'));
    return true;
  }

  // ---- the move modal
  //
  // Clicking a task's title asks where it goes next, then shows only the fields
  // that state needs. Everything is submitted through post(), so it takes the
  // same route as the screens and lands back on this page.
  // The title's href still points at Clarify, for no-JS and modifier-clicks.
  var CHOICES = [
    { key: 'i', state: 'inbox' }, { key: 'n', state: 'next' }, { key: 'l', state: 'later' },
    { key: 'w', state: 'waiting' }, { key: 's', state: 'scheduled' },
    { key: 'm', state: 'someday' }, { key: 'd', state: 'done' },
    { key: 'x', state: 'dropped' }, { key: 'f', state: 'filed' },
  ];

  function getList(url, field) {
    return fetch(url, { headers: { Accept: 'application/json' } })
      .then(function (r) { return r.ok ? r.json() : {}; })
      .then(function (j) { return j[field] || []; })
      .catch(function () { return []; });
  }

  function modalOpen() { return !!document.getElementById('move-modal'); }

  function openMove(task, direct) {
    if (modalOpen()) return;
    var base = '/ui/tasks/' + task.id;
    var contextsOn = document.body.getAttribute('data-contexts') === 'on';
    var projects = getList('/api/projects?status=active', 'projects');
    var contexts = contextsOn ? getList('/api/contexts', 'contexts') : null;

    var overlay = el('div', 'modal-overlay');
    overlay.id = 'move-modal';
    var box = el('div', 'modal move-modal');
    box.tabIndex = -1;
    overlay.appendChild(box);
    document.body.appendChild(overlay);

    var choices = CHOICES.filter(function (c) { return c.state !== task.state; });
    var step = 1, cur = 0;

    function close() { overlay.remove(); }

    overlay.addEventListener('click', function (e) { if (e.target === overlay) close(); });

    // Keys never reach the page's own shortcuts while the modal is open
    overlay.addEventListener('keydown', function (e) {
      e.stopPropagation();
      if (e.key === 'Escape') { e.preventDefault(); close(); return; }
      if (step !== 1 || e.metaKey || e.ctrlKey || e.altKey) return;
      if (e.key === 'j' || e.key === 'ArrowDown') { e.preventDefault(); showStep1(cur + 1); return; }
      if (e.key === 'k' || e.key === 'ArrowUp') { e.preventDefault(); showStep1(cur - 1); return; }
      if (e.key === 'Enter') { e.preventDefault(); choose(choices[cur]); return; }
      for (var i = 0; i < choices.length; i++) {
        if (choices[i].key === e.key) { e.preventDefault(); choose(choices[i]); return; }
      }
    });

    function showStep1(at) {
      step = 1;
      cur = Math.max(0, Math.min(choices.length - 1, at || 0));
      box.innerHTML = '';
      box.appendChild(el('div', 'modal-label', t('move.title', task.title)));
      var ul = el('ul', 'move-choices');
      choices.forEach(function (c, i) {
        var li = el('li', i === cur ? 'cur' : '');
        li.appendChild(el('span', 'kbd', c.key));
        li.appendChild(el('span', '', t('move.choice.' + c.state)));
        li.addEventListener('click', function () { choose(c); });
        ul.appendChild(li);
      });
      box.appendChild(ul);
      box.appendChild(el('div', 'modal-hint', t('move.hint')));
      box.focus();
    }

    function choose(c) {
      if (c.state === 'inbox' || c.state === 'someday') {
        rememberMove(task, c.state);
        post(base, { state: c.state });
        return;
      }
      if (c.state === 'done') { rememberMove(task, 'done'); post(base + '/complete'); return; }
      showStep2(c);
    }

    function field(form, labelKey, input) {
      var label = el('label', 'move-field');
      label.appendChild(el('span', '', t(labelKey)));
      label.appendChild(input);
      form.appendChild(label);
      return input;
    }

    function input(type, name, value, required) {
      var i = el('input');
      i.type = type; i.name = name; i.value = value || '';
      i.required = !!required;
      return i;
    }

    // A select filled once its list arrives. The current value is kept even
    // when the list does not have it (say, a project that is on hold), so
    // submitting never clears it by surprise.
    function select(name, list, labelOf, current, currentLabel, required) {
      var s = el('select');
      s.name = name; s.required = !!required;
      s.appendChild(new Option(t('move.none'), ''));
      list.then(function (items) {
        var seen = false;
        items.forEach(function (it) {
          if (String(it.id) === current) seen = true;
          s.appendChild(new Option(labelOf(it), String(it.id)));
        });
        if (current && !seen) s.appendChild(new Option(currentLabel || '#' + current, current));
        s.value = current;
      });
      return s;
    }

    function showStep2(c) {
      step = 2;
      box.innerHTML = '';
      box.appendChild(el('div', 'modal-label',
        t('move.title', task.title) + ' ' + t('move.choice.' + c.state)));
      var form = el('form', 'move-form');
      var path = base;
      var fixed = { state: c.state };
      var title = function (p) { return p.title; };
      var repeat = null;

      if (c.state === 'next') {
        field(form, 'move.project', select('project_id', projects, title,
          task.projectId, task.projectTitle, false));
        if (contexts) {
          field(form, 'move.context', select('context_id', contexts,
            function (x) { return x.name; }, task.contextId, '', false));
        }
      } else if (c.state === 'later') {
        field(form, 'move.project_required', select('project_id', projects, title,
          task.projectId, task.projectTitle, true));
      } else if (c.state === 'waiting') {
        field(form, 'move.waiting_for', input('text', 'waiting_for', task.waitingFor, true));
      } else if (c.state === 'scheduled') {
        repeat = repeatPicker(
          field(form, 'move.scheduled_on', input('date', 'scheduled_on', task.scheduledOn, true)),
          task.recurrence, task.recurrenceEndsOn);
        form.appendChild(repeat.node);
      } else if (c.state === 'dropped') {
        form.appendChild(el('p', 'move-confirm', t('move.confirm_drop', task.title)));
      } else if (c.state === 'filed') {
        path = base + '/file';
        fixed = {};
        field(form, 'move.file_title', input('text', 'title', task.title, true));
        field(form, 'move.file_tags', input('text', 'tags', '', false));
      }

      var actions = el('div', 'move-actions');
      var back = el('button', '', t('move.back'));
      back.type = 'button';
      back.addEventListener('click', function () { showStep1(0); });
      var submit = el('button', c.state === 'dropped' ? 'danger' : 'primary',
        c.state === 'dropped' ? t('move.choice.dropped') : t('move.submit'));
      submit.type = 'submit';
      actions.appendChild(back);
      actions.appendChild(submit);
      form.appendChild(actions);

      // The browser checks the required fields before this runs
      form.addEventListener('submit', function (e) {
        e.preventDefault();
        var fields = {};
        Object.keys(fixed).forEach(function (k) { fields[k] = fixed[k]; });
        Array.prototype.forEach.call(form.elements, function (f) {
          if (f.name) fields[f.name] = f.value;
        });
        if (repeat) {
          var r = repeat.value();
          Object.keys(r).forEach(function (k) { fields[k] = r[k]; });
        }
        if (c.state !== 'filed') rememberMove(task, c.state);
        post(path, fields);
      });
      // Enter on a select or a checkbox submits too, as it does in a text box
      form.addEventListener('keydown', function (e) {
        if (e.key === 'Enter' && (e.target.tagName === 'SELECT' || e.target.type === 'checkbox')) {
          e.preventDefault();
          form.requestSubmit();
        }
      });

      box.appendChild(form);
      var first = form.querySelector('input, select') || submit;
      first.focus();
    }

    var preset = direct && CHOICES.filter(function (c) { return c.state === direct; })[0];
    if (preset) showStep2(preset); else showStep1(0);
  }

  // The ✓ button on a row is an ordinary form; its completion can be undone too
  document.addEventListener('submit', function (e) {
    var form = e.target;
    if (!/\/complete$/.test(form.getAttribute('action') || '')) return;
    var task = taskOf(form.closest('li'));
    if (task) rememberMove(task, 'done');
  });

  // A plain click on a task's title opens the modal; a modifier-click or a
  // middle click still opens Clarify.
  document.addEventListener('click', function (e) {
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    var a = e.target.closest && e.target.closest('a.task-title');
    if (!a) return;
    var task = taskOf(a.closest('li'));
    if (!task) return;
    e.preventDefault();
    openMove(task);
  });

  document.addEventListener('keydown', function (ev) {
    if (ev.metaKey || ev.ctrlKey || ev.altKey) return;
    if (modalOpen()) return;

    if (inField(document.activeElement)) {
      if (ev.key === 'Escape') { document.activeElement.blur(); }
      return;
    }

    if (pendingG) {
      pendingG = false;
      clearTimeout(gTimer);
      var dest = { d: '/', w: '/wiki', i: '/gtd/inbox', n: '/gtd/next', p: '/gtd/projects',
                   l: '/gtd/day' }[ev.key];
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
      case 'u':
        if (undoToast) { ev.preventDefault(); runUndo(); }
        break;
      // The day page's links carry data-key; elsewhere these keys do nothing.
      // t renames the task under the cursor first, where there is one.
      case '[':
      case ']':
      case 't':
        if (ev.key === 't' && taskKey('t')) { ev.preventDefault(); break; }
        var nav = document.querySelector('a[data-key="' +
          { '[': 'prev-day', ']': 'next-day', t: 'today' }[ev.key] + '"]');
        if (nav) { ev.preventDefault(); window.location.assign(nav.getAttribute('href')); }
        break;
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
      setTimeout(function () { close(); refreshInbox(); }, 400);
    }).catch(function () {
      label.textContent = t("capture.failed");
    });
  });
}

// Screens that show the inbox count or list go stale after a modal capture, so
// they are reloaded - unless that would throw away something being typed.
var INBOX_PATHS = ['/', '/gtd', '/gtd/inbox', '/gtd/review'];

function refreshInbox() {
  var here = window.location.pathname;
  if (INBOX_PATHS.indexOf(here) < 0) return;
  if (document.querySelector('textarea, .modal-overlay')) return;
  var fields = document.querySelectorAll('input:not([type=hidden]), select');
  for (var i = 0; i < fields.length; i++) {
    var f = fields[i];
    if (f.type === 'checkbox' || f.type === 'radio') {
      if (f.checked !== f.defaultChecked) return;
    } else if (f.tagName === 'SELECT') {
      for (var j = 0; j < f.options.length; j++) {
        if (f.options[j].selected !== f.options[j].defaultSelected) return;
      }
    } else if (f.value !== f.defaultValue) {
      return;
    }
  }
  // Same marker as the wiki reload, so the scroll position carries over.
  try {
    sessionStorage.setItem('enghi:scroll:' + here,
                           JSON.stringify({ y: window.scrollY, t: Date.now() }));
  } catch (e) { /* give up in private mode and the like */ }
  window.location.reload();
}

// ---------------------------------------------------------------- pasting images
//
// Pasting or dropping an image into a textarea marked data-paste-upload (the
// article editor, the work log) uploads it there and then and inserts the
// Markdown at the cursor. Bound per textarea: Clarify has several.

document.querySelectorAll('textarea[data-paste-upload]').forEach(function (ta) {
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
});

// ---------------------------------------------------------------- Ctrl/Cmd+Enter
//
// Submits the form of a textarea marked data-ctrl-enter (the work log), going
// through validation like a click on its first button. A key the [[ completion
// already took (it picks a candidate on Enter) is left alone.
document.addEventListener('keydown', function (ev) {
  if (ev.key !== 'Enter' || !(ev.ctrlKey || ev.metaKey) || ev.isComposing || ev.defaultPrevented) return;
  var ta = ev.target;
  if (!ta.matches || !ta.matches('textarea[data-ctrl-enter]') || !ta.form) return;
  ev.preventDefault();
  if (ta.form.requestSubmit) ta.form.requestSubmit(); else ta.form.submit();
});

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

document.querySelectorAll('textarea[data-wikilink]').forEach(function (ta) {
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
});

// ---------------------------------------------------------------- the day page
//
// "Copy as Markdown" copies the text the server rendered into a hidden
// textarea, the same text /api/day?format=markdown answers. The Clipboard API
// is missing in some embedded browsers (the Emacs xwidget) and outside secure
// contexts, so the fallback shows the text selected, tries execCommand, and
// otherwise asks for a manual copy.

(function () {
  document.addEventListener('click', function (ev) {
    var btn = ev.target.closest && ev.target.closest('button[data-copy]');
    if (!btn) return;
    var ta = document.getElementById(btn.getAttribute('data-copy'));
    if (!ta) return;
    var status = btn.parentNode.querySelector('[data-copy-status]');
    function say(key) { if (status) status.textContent = t(key); }
    function fallback() {
      ta.hidden = false;
      ta.focus();
      ta.select();
      var ok = false;
      try { ok = document.execCommand('copy'); } catch (e) {}
      say(ok ? 'day.copied' : 'day.copy_manual');
    }
    if (navigator.clipboard && navigator.clipboard.writeText && window.isSecureContext) {
      navigator.clipboard.writeText(ta.value).then(function () { say('day.copied'); }, fallback);
    } else {
      fallback();
    }
  });
})();
