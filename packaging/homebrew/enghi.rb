# Homebrew formula for enghi.
#
# This file is the source of truth, but Homebrew does not read it directly. Copy it to
# `Formula/enghi.rb` in the separate `wakamenod/homebrew-tap` repository.
#
# **Builds from source rather than shipping prebuilt binaries** because:
#   - the formula always passes the sqlite_fts5 build tag, so it cannot be forgotten
#   - no downloaded binaries, so Gatekeeper never quarantines them
#   - a single formula covers both macOS and Linux
class Enghi < Formula
  desc "Local-only personal wiki and GTD server"
  homepage "https://github.com/wakamenod/enghi"
  url "https://github.com/wakamenod/enghi/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "a51fc90ea87b5b532d62f4bf792042b45a5b9cfae03eb202b771ec44aa03660c"
  license "MIT"
  head "https://github.com/wakamenod/enghi.git", branch: "main"

  depends_on "go" => :build

  def install
    # Search requires SQLite FTS5 and the trigram tokenizer, which are compile-time
    # options in the bundled C source, so cgo must be enabled.
    ENV["CGO_ENABLED"] = "1"
    system "go", "build", *std_go_args(ldflags: "-X main.version=#{version}"),
           "-tags", "sqlite_fts5", "./cmd/enghi"
  end

  # brew services registers this with launchd on macOS and systemd on Linux.
  # `enghi install-agent` remains the fallback for non-Homebrew users.
  service do
    run [opt_bin/"enghi", "serve"]
    keep_alive true
    log_path var/"log/enghi.log"
    error_log_path var/"log/enghi.err.log"
  end

  def caveats
    <<~EOS
      To run enghi in the background:
        brew services start enghi

      Then open http://127.0.0.1:7777/ and visit /guide to get started.
      Configuration lives in ~/.config/enghi/config.toml (defaults apply if absent).
    EOS
  end

  test do
    assert_match "enghi", shell_output("#{bin}/enghi version")

    # Verify the build includes FTS5: doctor creates the database and runs
    # integrity checks, so a missing tokenizer fails here.
    ENV["XDG_DATA_HOME"] = testpath/"data"
    ENV["XDG_CONFIG_HOME"] = testpath/"cfg"
    system bin/"enghi", "doctor"
  end
end
