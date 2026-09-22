-- 利用者が画面から切り替える設定。
--
-- config.toml(ポート・パス)とは別物である。あちらは起動時に読む運用の設定で、
-- 手で編集してサーバを再起動するもの。こちらは実行中に画面から変わる好みなので、
-- 他のデータと同じ DB に置き、バックアップとエクスポートの対象に自然に含める。
--
-- **行が無い = 既定値**とする。既定を DB に書き込まないので、
-- 既定を変えたいときはコード側を直せばよく、移行用の UPDATE が要らない。
CREATE TABLE settings (
  key        TEXT NOT NULL PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
