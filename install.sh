#!/bin/sh
# GitFolio installer for macOS and Linux — https://github.com/Alineteam-Inc/GitFolio
#
#   curl -fsSL https://raw.githubusercontent.com/Alineteam-Inc/GitFolio/main/install.sh | sh
#
# Downloads the release for this OS and CPU over HTTPS, verifies its SHA-256 checksum against the
# release's checksums.txt and refuses to install on any mismatch.
#
# Options (environment variables):
#   GITFOLIO_VERSION      release tag to install, e.g. v0.1.0 (default: latest)
#   GITFOLIO_INSTALL_DIR  where to put the binary (default: /usr/local/bin if writable, else ~/.local/bin)
#   GITFOLIO_BASE_URL     download location, for mirrors or testing (default: GitHub Releases)
set -eu

main() {
	repo="Alineteam-Inc/GitFolio"

	case "$(uname -s)" in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	*) fail "unsupported OS: $(uname -s) (macOS and Linux only)" ;;
	esac
	case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) fail "unsupported CPU: $(uname -m)" ;;
	esac

	if [ -n "${GITFOLIO_BASE_URL:-}" ]; then
		base=$GITFOLIO_BASE_URL
	elif [ -n "${GITFOLIO_VERSION:-}" ]; then
		base="https://github.com/$repo/releases/download/$GITFOLIO_VERSION"
	else
		base="https://github.com/$repo/releases/latest/download"
	fi
	case "$base" in
	https://* | file://*) ;;
	*) fail "refusing to download over an insecure connection: $base" ;;
	esac

	archive="gitfolio_${os}_${arch}.tar.gz"
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT

	echo "Downloading $archive ..."
	download "$base/$archive" "$tmp/$archive"
	download "$base/checksums.txt" "$tmp/checksums.txt"

	expected=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
	[ -n "$expected" ] || fail "$archive is not listed in checksums.txt; not installing"
	actual=$(sha256 "$tmp/$archive")
	[ "$expected" = "$actual" ] || fail "checksum mismatch for $archive (expected $expected, got $actual); not installing"
	echo "Checksum verified (SHA-256)."

	tar -xzf "$tmp/$archive" -C "$tmp" gitfolio

	dir=${GITFOLIO_INSTALL_DIR:-}
	if [ -z "$dir" ]; then
		if [ -w /usr/local/bin ]; then dir=/usr/local/bin; else dir="$HOME/.local/bin"; fi
	fi
	mkdir -p "$dir"
	cp "$tmp/gitfolio" "$dir/gitfolio"
	chmod 0755 "$dir/gitfolio"
	echo "Installed to $dir/gitfolio"

	case ":$PATH:" in
	*":$dir:"*) ;;
	*) case "${SHELL:-}" in
		*/zsh) profile="~/.zshrc" ;;
		*/bash) profile="~/.bashrc" ;;
		*) profile="~/.profile" ;;
		esac
		echo "Note: $dir is not in your PATH. Add this line to $profile:"
		echo "  export PATH=\"$dir:\$PATH\"" ;;
	esac
	echo
	echo "Next: run  gitfolio init  to choose the repositories to collect."
}

download() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --proto '=https,file' --tlsv1.2 -o "$2" "$1" || fail "download failed: $1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q --https-only -O "$2" "$1" || fail "download failed: $1"
	else
		fail "curl or wget is required"
	fi
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{ print $1 }'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{ print $1 }'
	else
		fail "sha256sum or shasum is required to verify the download"
	fi
}

fail() {
	echo "gitfolio install: $*" >&2
	exit 1
}

# Everything runs from here, so a download cut short in `curl | sh` executes nothing.
main "$@"
