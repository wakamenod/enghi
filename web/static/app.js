// enghi — 少量の vanilla JS。キーボード操作と focus チャネルの受信だけを担う。

// ---------------------------------------------------------------- 文言
//
// **JS の中に文言を直書きしない。**サーバがその言語の文言を body の
// data-strings に入れて渡すので、そこから引く。

var S = (function () {
  try {
    return JSON.parse(document.body.getAttribute("data-strings") || "{}");
  } catch (e) { return {}; }
})();

function t(key, arg) {
  var s = S[key] || key;
  return arg === undefined ? s : s.replace(/%[sd]/, arg);
}

// ---------------------------------------------------------------- focus チャネル
//
// DESIGN 4.3: Emacs で検索・選択 → 別ディスプレイに開きっぱなしのブラウザが追従する。
// **指数バックオフの自動再接続を必ず実装すること**(DESIGN 8-8)。
// サーバ再起動後に張り直されないと、常駐運用で「なぜか focus が飛ばない」状態になる。

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
    if (ws) return;            // 二重接続を作らない
    try {
      ws = new WebSocket(url());
    } catch (e) {
      schedule();
      return;
    }

    ws.onopen = function () {
      backoff = 500;           // 接続できたらバックオフを戻す
    };

    ws.onmessage = function (ev) {
      var msg;
      try { msg = JSON.parse(ev.data); } catch (e) { return; }
      if (msg.type === 'navigate' && msg.path) {
        if (window.location.pathname + window.location.search !== msg.path) {
          window.location.assign(msg.path);
        }
      }
      // 表示中のページが他の経路で更新されたら読み込み直す(編集中は触らない)
      if (msg.type === 'updated' && msg.slug) {
        // **pathname はパーセントエンコード済み、slug は生。**日本語のスラグでは
        // そのまま比べると必ず外れるので、復号した方とも突き合わせる。
        var here = window.location.pathname;
        var hereDecoded = here;
        try { hereDecoded = decodeURIComponent(here); } catch (e) { /* 壊れた URL */ }
        if ((here === '/wiki/' + msg.slug || hereDecoded === '/wiki/' + msg.slug) &&
            !document.querySelector('textarea')) {
          // Emacs で編集しながら見ている場合、読み直しのたびに先頭へ戻ると使えない。
          // 読む位置を持ち越す(復帰は下の restoreScroll)。
          try {
            sessionStorage.setItem('enghi:scroll:' + here,
                                   JSON.stringify({ y: window.scrollY, t: Date.now() }));
          } catch (e) { /* private mode などでは諦める */ }
          window.location.reload();
        }
      }
    };

    // **指数バックオフの自動再接続は必須**(DESIGN 8-8)。
    // サーバ再起動後に張り直されないと、常駐運用で
    // 「なぜか focus が飛ばない」状態になる。
    ws.onclose = function () { ws = null; schedule(); };
    ws.onerror = function () { if (ws) { ws.close(); } };
  }

  function disconnect() {
    if (ws) {
      ws.onclose = null;       // 意図的な切断では再接続を仕掛けない
      ws.close();
      ws = null;
    }
  }

  function schedule() {
    if (timer) return;         // 保留中の再接続が既にあるなら足さない
    timer = setTimeout(connect, backoff);
    backoff = Math.min(backoff * 2, BACKOFF_MAX);
  }

  // ページを離れるときは閉じる。bfcache に残ったページが接続を掴んだままにならないように。
  window.addEventListener('pagehide', function () {
    if (timer) { clearTimeout(timer); timer = null; }
    disconnect();
  });

  // bfcache から復帰したら張り直す。
  window.addEventListener('pageshow', function () {
    backoff = 500;
    if (!ws && !timer) connect();
  });

  connect();
})();

// ---------------------------------------------------------------- 読む位置の持ち越し
//
// 上の updated が仕掛けた読み直しのときだけ、直前のスクロール位置に戻す。
// 普通の遷移や再読込を巻き込まないよう、印は一度使ったら消し、古いものは捨てる。

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
  if (!saved || Date.now() - saved.t > 10000) return;   // 10 秒より古い印は使わない
  window.scrollTo(0, saved.y);
})();

// ---------------------------------------------------------------- キーボード操作
//
// DESIGN 6: キーボード操作を第一級に扱う。
//   /      … 検索にフォーカス
//   g d/w/i/n/p … ダッシュボード / Wiki / Inbox / Next / Projects
//   e      … 表示中の記事を編集
//   j / k  … リスト内移動、Enter で開く

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

  // ---- カーソル行のタスクを1キーで動かす
  //
  // 状態変更の口はサーバに揃っているので、ここは form を1つ作って投げるだけ。
  // **fetch ではなく form 送信にする。**Origin と Sec-Fetch-Site が付き、
  // 既存の画面と同じ経路(secure ミドルウェアの formAllowed)を通る。
  //
  // `k' は既にカーソルの上移動なので、agenda で `k' だった「今回は飛ばす」は
  // `S' に逃がしてある。
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
    if (!id) return null;                       // 記事の一覧など、タスクでない行
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
      // 破棄だけは戻せないので確認する
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

// ---------------------------------------------------------------- テーマの切り替え
//
// auto(OS の設定に従う) → light → dark → auto の順に回す。
// 選択は localStorage に持つ。auto のときは属性を外して CSS の
// prefers-color-scheme に任せる。

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

// ---------------------------------------------------------------- クイックキャプチャ
//
// DESIGN 6: c … どこからでも Inbox へ1行追加するモーダル。
// 頭の中を空にする操作は、どの画面からでも1打鍵で始まること。

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

// ---------------------------------------------------------------- 画像の貼り付け
//
// 編集中のテキストエリアに画像を貼る/落とすと、その場でアップロードして
// Markdown の記法をカーソル位置に差し込む。

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
      // 仮置きの文字列を実際のリンクに差し替える
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
      // 応答が返るまでの間、どこに入るかが分かるようにしておく
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

// ---------------------------------------------------------------- 編集画面のプレビュー
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

// ---------------------------------------------------------------- [[...]] の補完
//
// 編集中に `[[` を打つと、タイトルと別名の一覧がキャレットの位置に出る。
// タイトルを覚えていなくてもリンクが張れること。打ち間違いで静かに未解決リンクに
// 落ちるのを防ぐのが主目的なので、**候補は必ず解決されるものだけ**を出す
// (サーバ側で page_titles だけを引いている)。
//
// 候補が無いときは、入力中の文字列をそのまま挿入する行だけを出す。
// 未解決リンクは「これから書く記事」を示す正当な使い方なので、塞がない(DESIGN 2.1)。

(function () {
  var ta = document.querySelector('textarea[name=body]');
  if (!ta) return;

  var box = null;     // 候補の入れ物
  var items = [];     // 候補(最後の1件は「そのまま挿入」のことがある)
  var sel = 0;        // 選択位置
  var open = 0;       // 本文中の `[[` の開始位置
  var timer = null;
  var seq = 0;        // 応答の追い越し対策。打鍵が速いと古い応答が後から届く

  // キャレットの画面位置。textarea には位置を取る API が無いので、
  // 同じ体裁の隠し要素に同じ文字を流し込んで測る。
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

  // キャレット直前の `[[` を探す。閉じ括弧・改行・`|` を跨いだら補完しない
  // (`|` の後はラベルであってタイトルではない)。
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
        if (my !== seq) return;          // 追い越された応答は捨てる
        var now = context();
        if (!now || now.start !== open) { close(); return; }
        items = (res.titles || []).map(function (v) { return v; });
        // 完全一致が既にあるなら「そのまま挿入」は出さない(同じ行が二重に並ぶ)
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
    timer = setTimeout(refresh, 100);   // 検索窓と同じ間隔
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
      close(); ev.preventDefault(); ev.stopPropagation();   // クイックキャプチャに渡さない
    }
  });
})();
