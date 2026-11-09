#!/usr/bin/env bash
# Installs or updates leitner from a GitHub release. Run it as root on the
# Arch laptop, from ~/leitner/deploy (see deploy/README.md):
#   sudo bash update.sh          the latest release
#   sudo bash update.sh v0.1.0   that release (also to roll back)
#
# Downloads leitner-linux and leitner-linux.sha256 with the read-only token,
# verifies the checksum, backs up the database, installs /opt/leitner/leitner
# (the previous binary stays as leitner.prev) and restarts the service. If the
# new binary doesn't come up, it puts the previous one back.
set -euo pipefail

REPO=Damba017/leitner
ASSET=leitner-linux
# One line, "Authorization: Bearer <token>", root-only (README, "Token file").
AUTH_FILE=/etc/leitner/github-auth
BIN_DIR=/opt/leitner
BIN=$BIN_DIR/leitner
DB=/var/lib/leitner/leitner.db
BACKUP_DIR=/var/lib/leitner/backups
KEEP_BACKUPS=5
UNIT=/etc/systemd/system/leitner.service
# Must match -addr in the unit. /login answers without a session.
HEALTH_URL=http://127.0.0.1:8370/login
# The unit in the repo, next to this script; only used to warn when it changed.
SRC_UNIT=$(dirname -- "$(readlink -f -- "$0")")/leitner.service

die() {
	echo "update.sh: $*" >&2
	exit 1
}

[[ $EUID -eq 0 ]] || die "run it as root: sudo bash $0 [tag]"
[[ $# -le 1 ]] || die "usage: update.sh [tag]"
[[ $# -eq 0 || $1 =~ ^[A-Za-z0-9._-]+$ ]] || die "unexpected tag: $1"
[[ -s $AUTH_FILE ]] || die "$AUTH_FILE is missing or empty (README, \"Token file\")"
id -u leitner >/dev/null 2>&1 || die "system user leitner is missing (README, \"System user\")"

# runuser below starts in this directory as leitner, who can't read the caller's home.
cd /
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# github <Accept header> <API path under the repo> <output file>
# The token is read from the file, so it never shows up in the process list.
# curl doesn't send it along when GitHub redirects a download to its storage.
github() {
	curl --fail --silent --show-error --location --retry 3 \
		--proto '=https' --proto-redir '=https' \
		--header "@$AUTH_FILE" \
		--header "Accept: $1" \
		--header 'X-GitHub-Api-Version: 2022-11-28' \
		--output "$3" "https://api.github.com/repos/$REPO/$2" ||
		die "GitHub request $2 failed (401: token wrong or expired; 404: no such release, or no access)"
}

# asset_id <name>: the API id of the release asset with that name. Without jq:
# in the release JSON each asset's API url comes just before its name.
asset_id() {
	local id='' line re='^"name": *"(.*)"$'
	while IFS= read -r line; do
		if [[ $line == '"url"'* ]]; then
			id=${line%\"}
			id=${id##*/}
		elif [[ -n $id && $line =~ $re ]]; then
			if [[ ${BASH_REMATCH[1]} == "$1" ]]; then
				echo "$id"
				return 0
			fi
			id=''
		fi
	done < <(grep -oE \
		-e '"url": *"https://api\.github\.com/repos/[^"]+/releases/assets/[0-9]+"' \
		-e '"name": *"[^"]*"' "$tmp/release.json")
	return 1
}

# healthy: true once the service answers on HEALTH_URL (within ~20 s) and is
# still active 2 s later. A binary that fails at start (e.g. a database schema
# newer than the binary) never listens, so this stays false.
healthy() {
	local i
	for i in {1..20}; do
		sleep 1
		if curl --silent --fail --output /dev/null --max-time 2 "$HEALTH_URL"; then
			sleep 2
			systemctl is-active --quiet leitner
			return
		fi
	done
	return 1
}

# normalize_unit <file>: the unit without its comments and Funnel name, so a
# filled-in unit compares equal to the one in the repo.
normalize_unit() {
	grep -v '^#' "$1" | sed -E 's|https://[^ ]*\.ts\.net|https://FUNNEL|'
}

if [[ $# -eq 1 ]]; then release=releases/tags/$1; else release=releases/latest; fi
echo "Looking up $release..."
github application/vnd.github+json "$release" "$tmp/release.json"
tag=$(grep -oE '"tag_name": *"[^"]*"' "$tmp/release.json" | sed -E 's/.*"([^"]*)"$/\1/') ||
	die "no tag_name in the release info"
bin_id=$(asset_id "$ASSET") || die "release $tag has no $ASSET"
sum_id=$(asset_id "$ASSET.sha256") || die "release $tag has no $ASSET.sha256"

echo "Downloading $ASSET from $tag..."
github application/octet-stream "releases/assets/$bin_id" "$tmp/$ASSET"
github application/octet-stream "releases/assets/$sum_id" "$tmp/$ASSET.sha256"

# The checksum file is shasum's output: "<64 hex>  leitner-linux".
want=$(sed -nE "s/^([0-9a-f]{64}) [ *]$ASSET\$/\1/p" "$tmp/$ASSET.sha256")
[[ -n $want ]] || die "$ASSET.sha256 has no checksum line for $ASSET"
got=$(sha256sum "$tmp/$ASSET" | cut -d' ' -f1)
[[ $got == "$want" ]] || die "checksum mismatch for $ASSET: expected $want, got $got"
echo "Checksum OK."
chmod 755 "$tmp/$ASSET"
new_version=$("$tmp/$ASSET" version) || die "the downloaded $ASSET doesn't run on this machine"
echo "Downloaded $new_version."
# Go stamps the tag into the binary only when it was built on the tagged commit.
[[ $new_version == "leitner $tag "* ]] ||
	echo "Note: the binary says \"$new_version\", not $tag: it was built before tagging, from another commit, or from a dirty tree."

if [[ -f $BIN && $(sha256sum "$BIN" | cut -d' ' -f1) == "$got" ]]; then
	echo "$BIN is already $tag; nothing to do."
	exit 0
fi

backup=''
if [[ -f $DB ]]; then
	[[ -x $BIN ]] || die "$DB exists but $BIN is missing, so no backup can be made"
	install -d -o leitner -g leitner -m 700 "$BACKUP_DIR"
	backup=$BACKUP_DIR/leitner-$(date +%Y%m%d-%H%M%S).db
	# As leitner, so no database file (WAL, shm) ever ends up owned by root.
	runuser -u leitner -- "$BIN" backup -db "$DB" "$backup"
	# Keep the newest $KEEP_BACKUPS; the names sort by date. The nightly
	# backups live in backups/nightly/ and are left alone.
	backups=("$BACKUP_DIR"/leitner-*.db)
	if ((${#backups[@]} > KEEP_BACKUPS)); then
		rm -f -- "${backups[@]:0:${#backups[@]}-KEEP_BACKUPS}"
	fi
else
	echo "No database yet ($DB), so no backup."
fi

install -d -o root -g root -m 755 "$BIN_DIR"
prev_version=''
if [[ -f $BIN ]]; then
	prev_version=$("$BIN" version 2>/dev/null) || prev_version='the previous binary'
	cp -p "$BIN" "$BIN.prev"
fi
# Write next to the target, then rename: the swap is atomic.
install -o root -g root -m 755 "$tmp/$ASSET" "$BIN.new"
mv -f "$BIN.new" "$BIN"
echo "Installed $tag as $BIN."

if [[ ! -f $UNIT ]]; then
	echo "No $UNIT yet: continue with the README, \"Unit\"."
	exit 0
fi
if [[ -f $SRC_UNIT ]] && ! cmp -s <(normalize_unit "$SRC_UNIT") <(normalize_unit "$UNIT"); then
	echo "Note: $SRC_UNIT differs from $UNIT. Reinstall the unit after this update (README, \"Unit\")."
fi

since=$(date '+%Y-%m-%d %H:%M:%S')
echo "Restarting leitner..."
systemctl restart leitner
if healthy; then
	journalctl -u leitner --since "$since" -n 5 --no-pager
	echo "OK: $new_version is running."
	exit 0
fi

journalctl -u leitner --since "$since" -n 20 --no-pager
[[ -n $prev_version ]] || die "FAILED: $tag doesn't come up, and there was no previous binary to go back to"
echo "update.sh: $tag doesn't come up; putting $prev_version back..." >&2
install -o root -g root -m 755 "$BIN.prev" "$BIN.new"
mv -f "$BIN.new" "$BIN"
since=$(date '+%Y-%m-%d %H:%M:%S')
systemctl restart leitner
if healthy; then
	die "FAILED: $tag didn't come up. Rolled back: $prev_version is running again."
fi
journalctl -u leitner --since "$since" -n 20 --no-pager
die "FAILED: $tag didn't come up, and $prev_version doesn't either. See the README, \"Roll back\".${backup:+ Database backup from just before this update: $backup}"
