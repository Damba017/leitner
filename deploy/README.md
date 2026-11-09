# Deploy

leitner runs on the Arch laptop as a systemd service on `127.0.0.1:8370`. Tailscale Funnel makes it public over HTTPS at `https://NAME.TAILNET.ts.net:8443`, next to nestje, which keeps port 443 on the same name. The Mac builds the Linux binary and publishes it as a GitHub Release. The Arch laptop downloads it with a read-only token. Nothing is built on Arch.

Placeholders below: `NAME.TAILNET.ts.net` is the laptop's Funnel name (the one nestje already uses), and `vX.Y.Z` is a release tag.

| What | Where |
| --- | --- |
| Binary, previous binary | `/opt/leitner/leitner`, `/opt/leitner/leitner.prev` |
| Database | `/var/lib/leitner/leitner.db` (folder mode 0700, owner `leitner`) |
| Update backups (5 newest) | `/var/lib/leitner/backups/leitner-<date>-<time>.db` |
| Nightly backups (7 newest) | `/var/lib/leitner/backups/nightly/` |
| Token file | `/etc/leitner/github-auth` (root only) |
| Unit | `/etc/systemd/system/leitner.service` |

## 1. Prerequisites on Arch

These are already in place for nestje:
- systemd, `git`, `curl`, coreutils and util-linux (`runuser`), all from the base system.
- Tailscale, logged in, with MagicDNS, HTTPS certificates, Funnel approved and key expiry disabled for this laptop.
- The laptop stays on: system Wi-Fi connection, and no sleep with the lid closed (nestje's DEPLOY.md, "Always on").

For leitner you only add a system user, a token, the binary, a unit and a second Funnel port.

## 2. Mac: build and publish a release

Run this in the repo, on a clean `main` that is pushed:

```sh
git status                    # nothing to commit, no untracked files, up to date with origin/main
go vet ./... && go test ./...
git tag vX.Y.Z
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o leitner-linux .
shasum -a 256 leitner-linux > leitner-linux.sha256
go version -m leitner-linux | grep -E '^\s+mod'     # must say vX.Y.Z, without +dirty
git push origin vX.Y.Z
```

- Tag first, then build. Go only writes the tag into the binary when the build runs on the tagged commit in a clean tree. Otherwise `leitner version` shows something like `v0.0.0-20261109…-abc1234+dirty`, and `update.sh` warns about it.
- `GOOS=linux GOARCH=amd64` targets the x86_64 laptop instead of the Mac. `CGO_ENABLED=0` makes one static binary that needs no C libraries. `-trimpath` leaves the Mac's folder names out of the binary.
- `shasum -a 256` writes `<hash>  leitner-linux`, which `update.sh` checks.
- `go version -m` prints what Go embedded. The `mod` line holds the version.
- Both files are in `.gitignore`. Never commit them.

On github.com, open the repo → Releases → "Draft a new release":
1. Choose the tag you just pushed.
2. Give it a title (the tag) and an optional line about what changed.
3. Drag `leitner-linux` and `leitner-linux.sha256` into the assets box, and wait until both have uploaded.
4. Leave "Set as a pre-release" off, because `update.sh` takes the latest full release. Click "Publish release".

Then remove the local files: `rm leitner-linux leitner-linux.sha256`.

## 3. First install on Arch

### 3.1 Files

```sh
git clone https://github.com/Damba017/leitner.git ~/leitner
git -C ~/leitner config credential.helper store
```

- The clone holds `deploy/` (this file, the unit and `update.sh`). The Go source comes along too, which is harmless.
- `credential.helper store` (only for this repo, because of `-C`) is for when the repo goes private. The next `git pull` then asks for a username (`Damba017`) and a password: paste the read-only token from 3.3. git saves both in `~/.git-credentials` and doesn't ask again. While the repo is public, git never asks.

To refresh the files later: `git -C ~/leitner pull`.

### 3.2 System user

```sh
sudo useradd --system --user-group --home-dir /var/lib/leitner --no-create-home --shell /usr/bin/nologin leitner
id leitner
```

- `--system` makes a system account (low uid, no password, no aging).
- `--user-group` also creates a group `leitner`.
- `--home-dir` with `--no-create-home` sets the home to the data folder without creating it. systemd creates it with the right owner (`StateDirectory`).
- `--shell /usr/bin/nologin` means nobody can log in as this user.

### 3.3 Token file

Create the token on the Mac, where you're logged in to github.com. Go to Settings → Developer settings → Personal access tokens → Fine-grained tokens → Generate new token:
- Name: `leitner-arch`. Expiration: 90 days.
- Repository access: "Only select repositories" → `Damba017/leitner`.
- Permissions → Repository permissions → **Contents: Read-only**. GitHub adds "Metadata: Read-only" by itself. Nothing else.

Send it to the laptop over a private channel you already use on both machines, and delete that message once it works. Paste it at the prompt of the second command (Ctrl+Shift+V in most Linux terminals). It isn't shown and doesn't end up in your shell history.

```sh
sudo install -d -m 700 /etc/leitner
sudo bash -c 'umask 077; read -rsp "GitHub token: " t; echo; printf "Authorization: Bearer %s\n" "$t" > /etc/leitner/github-auth'
sudo ls -l /etc/leitner/github-auth        # must be -rw------- root root
sudo curl -fsS -o /dev/null -w '%{http_code}\n' -H @/etc/leitner/github-auth https://api.github.com/repos/Damba017/leitner
```

- `install -d -m 700` creates the folder, readable by root only. `umask 077` makes the new file mode 600.
- `read -rsp` reads silently (`-s`) with a prompt (`-p`), and keeps backslashes as typed (`-r`).
- The file holds one curl header line. `update.sh` passes it with `curl -H @file`, so the token never shows up in the process list.
- The last command checks the token: expect `200`. `-f` fails on an HTTP error, `-sS` stays quiet except for errors, `-o /dev/null` drops the body, and `-w` prints the status.

When the token expires, `update.sh` stops with a 401. Make a new one on the Mac and run the second command again. If git stored the old one (3.1), remove its line from `~/.git-credentials` so git asks again.

### 3.4 Binary

```sh
cd ~/leitner/deploy && sudo bash update.sh
/opt/leitner/leitner version
```

The first run downloads the latest release, checks the checksum and installs `/opt/leitner/leitner`. There's no database or unit yet, so it makes no backup and doesn't restart anything.

### 3.5 Unit

```sh
sudo install -m 644 ~/leitner/deploy/leitner.service /etc/systemd/system/leitner.service
sudo sed -i 's|<machine>.<tailnet>.ts.net|NAME.TAILNET.ts.net|' /etc/systemd/system/leitner.service
grep ExecStart /etc/systemd/system/leitner.service
sudo systemd-analyze verify /etc/systemd/system/leitner.service
sudo systemctl daemon-reload
sudo systemctl enable --now leitner
systemctl status leitner
journalctl -u leitner -n 20 --no-pager
systemd-analyze security leitner.service
```

- `install -m 644` copies the unit, readable for everyone, owned by root.
- `sed -i` edits it in place. `ExecStart` must end in `-url https://NAME.TAILNET.ts.net:8443`: no `<…>` left, no trailing slash or dot, and `:8443` kept. The `-url` also marks the session cookie Secure; push and the service worker (later phases) need it to match the Funnel URL exactly.
- `systemd-analyze verify` reports typos and unknown options.
- `daemon-reload` makes systemd read the new file. `enable --now` starts the service now and at every boot.
- The journal should show `listening addr=127.0.0.1:8370 … version=vX.Y.Z`.
- `systemd-analyze security` scores the sandbox (nestje's unit scores the same). If the sandbox blocks something the app needs, the journal shows it (e.g. "operation not permitted"). Then adjust that one option, with a reason, instead of dropping the hardening.

If `deploy/leitner.service` changes later (`update.sh` says so), install it again the same way, including the `sed`, then run `sudo systemctl daemon-reload && sudo systemctl restart leitner`.

### 3.6 First admin

```sh
sudo -u leitner /opt/leitner/leitner create-admin -db /var/lib/leitner/leitner.db
```

It asks for a username, email and password (twice). Always run the CLI as `leitner` (section 5).

### 3.7 Funnel on port 8443

```sh
sudo tailscale funnel --bg --https=8443 http://127.0.0.1:8370
tailscale funnel status
curl -sI https://NAME.TAILNET.ts.net:8443/
curl -sI https://NAME.TAILNET.ts.net/
```

- `--bg` keeps it on in the background, also after a reboot or a `tailscale down`/`up`.
- `--https=8443` adds a second port next to nestje's 443. Funnel only allows 443, 8443 and 10000.
- The target must be `http://127.0.0.1:8370`, which is what the app listens on.
- `tailscale funnel status` (no `sudo` needed) must list both `:443` (nestje) and `:8443` (leitner).
- `curl -sI` fetches only the headers. The first must give a `303` to `/login` (leitner), the second still nestje.
- If the first requests fail with a TLS error, wait a few minutes and try again (see nestje's DEPLOY.md, "Funnel").

Then open `https://NAME.TAILNET.ts.net:8443/` on the iPhone over mobile data (not Wi-Fi) and log in.

## 4. Checks after the first start

Write down the result of each check. Anything that needs a code change goes to a Mac session; don't patch on Arch.

- **Login through Funnel.** Logging in on the iPhone must work. If the browser shows a 403 on login, Funnel changed `Origin` or `Host`, and the CSRF check in `server.go` (`http.CrossOriginProtection`) needs a look.
- **Client IP.** `clientIP` (auth.go) trusts the last `X-Forwarded-For` entry, but only on connections from 127.0.0.1 or ::1 (tailscaled). Log in once with a wrong password from the iPhone (mobile data), then run:
  ```sh
  journalctl -u leitner -g 'login failed' -n 5 --no-pager
  ```
  `-g` keeps only matching lines. `ip=` should be the phone's public address. If it's `127.0.0.1`, Funnel sends no `X-Forwarded-For`: then all visitors share one limit of 5 failed logins per 15 minutes, and `clientIP` needs a change.
- **Login speed.** This also counts as one failed login from the laptop's address:
  ```sh
  curl -s -o /dev/null -w '%{http_code} %{time_total}\n' -H 'Origin: https://NAME.TAILNET.ts.net:8443' -d 'login=nobody&password=wrong' https://NAME.TAILNET.ts.net:8443/login
  ```
  Expect `401` and a time well under 1 s: that's argon2 on this CPU. `-d` sends the form as a POST, and the `Origin` must include `:8443`. A `403` within a few milliseconds means the CSRF check refused it.
- **nestje still works.** Open `https://NAME.TAILNET.ts.net/` and log in to nestje. Both apps on one name share cookies, so they use different cookie names (`leitner_session`). Logging in to one must not log you out of the other.
- **Backup.** Make a test copy, check it, and remove it:
  ```sh
  sudo -u leitner /opt/leitner/leitner backup -db /var/lib/leitner/leitner.db /var/lib/leitner/test-backup.db
  sudo -u leitner /opt/leitner/leitner list-users -db /var/lib/leitner/test-backup.db
  sudo bash -c 'rm -v /var/lib/leitner/test-backup.db*'
  ```
  `rm` runs in a root shell because of the `*`: your own shell can't read `/var/lib/leitner` (0700), so it would pass the literal name to `sudo rm`. `-v` prints each removed file.
- **Update and roll back.** After the first real update (section 6), also try a rollback with `sudo bash update.sh vX.Y.Z` to the older release, and then update to the latest again.
- **Reboot.** Reboot the laptop, don't log in, and wait a few minutes. Then `systemctl is-active leitner` should say `active`, `tailscale funnel status` should still list `:8443`, and the app should open on the iPhone over mobile data.

## 5. Users

```sh
sudo -u leitner /opt/leitner/leitner create-admin -db /var/lib/leitner/leitner.db
sudo -u leitner /opt/leitner/leitner reset-password -db /var/lib/leitner/leitner.db NAME   # asks for the new password
sudo -u leitner /opt/leitner/leitner list-users -db /var/lib/leitner/leitner.db
```

NAME is a username or email. Everything else (registration on/off, admin rights, deleting users) is on `/admin`.

Always run these as `leitner` (`sudo -u leitner`), also while the service runs. SQLite creates `leitner.db-wal` and `-shm` next to the database. If root creates them, the service can no longer write and every change fails.

## 6. Update

1. On the Mac: section 2, with a new tag.
2. On Arch:
   ```sh
   cd ~/leitner && git pull
   cd deploy && sudo bash update.sh
   ```
   If `update.sh` says the unit changed, reinstall it afterwards (3.5).

What `update.sh` does:
1. It downloads the latest release (or the tag you give it) and checks the checksum. On any download or checksum error it stops without changing anything. If that release is already installed, it stops there.
2. As `leitner`, it backs up the database to `/var/lib/leitner/backups/` and keeps the 5 newest.
3. It keeps the current binary as `leitner.prev`, swaps in the new one atomically and restarts the service.
4. It waits up to 20 s for `http://127.0.0.1:8370/login` to answer. If that doesn't happen, it puts `leitner.prev` back, restarts, and ends with `FAILED: …`, saying whether the old version runs again.

The new binary migrates the database when it starts, after the backup.

**Nightly backups.** The running service also makes a backup every night at 03:30 (Amsterdam time) in `backups/nightly/` and keeps the 7 newest. If the laptop was off at 03:30, it makes one right after the next start. The update backups and the nightly ones never delete each other's files. Nothing leaves the laptop automatically.

## 7. Roll back

**To an earlier release.** This is enough if the newer release didn't change the database:

```sh
cd ~/leitner/deploy && sudo bash update.sh vX.Y.Z
```

It makes a backup first, like a normal update. If the newer release did migrate the database, the older binary refuses to start (`database schema version N is newer than this binary` in the journal). `update.sh` then puts the newer binary back and reports `FAILED`. Nothing is lost, but to really go back you also need the database from before that update:

```sh
sudo systemctl stop leitner
sudo ls -l /var/lib/leitner/backups/ /var/lib/leitner/backups/nightly/
sudo bash -c 'd=/var/lib/leitner/replaced-$(date +%Y%m%d-%H%M%S); mkdir "$d" && mv /var/lib/leitner/leitner.db* "$d"/'
sudo install -o leitner -g leitner -m 600 /var/lib/leitner/backups/leitner-YYYYMMDD-HHMMSS.db /var/lib/leitner/leitner.db
cd ~/leitner/deploy && sudo bash update.sh vX.Y.Z
```

- Stop the service first, so the newer binary can't migrate the restored database again.
- Pick the backup made just before the update you're undoing. Changes made after it are lost.
- The `mv` moves the current database and its `-wal`/`-shm` files into a `replaced-…` folder instead of deleting them. Delete that folder once everything works. A stale `-wal` next to a restored database would corrupt it, which is why all three files have to move.
- `install -o leitner -g leitner -m 600` copies the backup into place, owned by the service user.
- `update.sh vX.Y.Z` installs the older release and starts it. Its backup step only reads the database and never migrates it. If it says "nothing to do" (that release is already installed), run `sudo systemctl start leitner`.

## 8. Removing leitner from the laptop

**Only when the user asks for it**, never as part of the setup or an update. nestje keeps running.

```sh
sudo tailscale funnel --https=8443 off
sudo systemctl disable --now leitner
sudo rm /etc/systemd/system/leitner.service && sudo systemctl daemon-reload
sudo rm -r /opt/leitner
sudo rm -r /etc/leitner
sudo rm -r /var/lib/leitner      # all data and backups
sudo userdel leitner
rm -rf ~/leitner
```

Also remove the `github.com` line for this token from `~/.git-credentials` (if git stored it), and delete the token on github.com.
