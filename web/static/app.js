// enghi — 少量の vanilla JS。キーボード操作と focus チャネルの受信だけを担う。

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
        var here = window.location.pathname;
        if (here === '/wiki/' + msg.slug && !document.querySelector('textarea')) {
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
