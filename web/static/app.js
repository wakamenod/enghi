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
