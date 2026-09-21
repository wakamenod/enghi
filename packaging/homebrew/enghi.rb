# enghi の Homebrew formula。
#
# 置き場はこのリポジトリではなく、別リポジトリ `wakamenod/homebrew-tap` の
# `Formula/enghi.rb`。ここにあるのは正本で、tap へはコピーして使う。
#
# **ビルド済みバイナリではなくソースからビルドする。** 理由:
#   - build tag sqlite_fts5 の付け忘れが構造的に起きない(formula が必ず付ける)
#   - ダウンロードした実行ファイルではないので、Gatekeeper の quarantine を踏まない
#   - macOS / Linux を 1 つの formula で賄える
class Enghi < Formula
  desc "Local-only personal wiki and GTD server"
  homepage "https://github.com/wakamenod/enghi"
  url "https://github.com/wakamenod/enghi/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000" # リリース後に埋める
  license "MIT"
  head "https://github.com/wakamenod/enghi.git", branch: "main"

  depends_on "go" => :build

  def install
    # SQLite の FTS5 と trigram tokenizer が要る(DESIGN 1)。
    # cgo 経由で同梱の C をビルドするため CGO_ENABLED=1 が必須。
    ENV["CGO_ENABLED"] = "1"
    system "go", "build", *std_go_args(ldflags: "-X main.version=#{version}"),
           "-tags", "sqlite_fts5", "./cmd/enghi"
  end

  # brew services が macOS では launchd、Linux では systemd に振り分ける。
  # enghi install-agent は brew を使わない人向けの裏口として残してある。
  service do
    run [opt_bin/"enghi", "serve"]
    keep_alive true
    log_path var/"log/enghi.log"
    error_log_path var/"log/enghi.err.log"
  end

  def caveats
    <<~EOS
      常駐させるには:
        brew services start enghi

      起動したら http://127.0.0.1:7777/ を開く。
      設定は ~/.config/enghi/config.toml (無ければ既定値で動く)。
    EOS
  end

  test do
    assert_match "enghi", shell_output("#{bin}/enghi version")

    # FTS5 の無いバイナリを掴んでいないことを確かめる。
    # doctor は DB を作って整合性を検査するので、tokenizer が無ければここで落ちる。
    ENV["XDG_DATA_HOME"] = testpath/"data"
    ENV["XDG_CONFIG_HOME"] = testpath/"cfg"
    system bin/"enghi", "doctor"
  end
end
