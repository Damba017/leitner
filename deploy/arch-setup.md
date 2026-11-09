# First install on the Arch laptop: brief for Claude Code

You are Claude Code, started on the Arch laptop in `~/leitner` (a clone of this repo). Your job: guide the user
through the first install of leitner **v0.1.0**, step by step, following `deploy/README.md`. Read that file first.

## Rules
- Always talk to the user in Dutch.
- Never run `sudo` and never change system config yourself (users, systemd, Tailscale, files under /etc, /opt, /var).
  Give the user one step at a time: the exact commands, what each command and flag does, and what output to expect.
  Wait for the output before the next step.
- You may run read-only commands without sudo yourself (`id`, `systemctl status`, `systemctl is-active`,
  `journalctl`, `tailscale funnel status`, `curl -sI`). If `journalctl` shows nothing, the user's account
  can't read the system journal: give the user the `sudo journalctl …` variant instead.
- Never install anything. Everything needed is already on the laptop (systemd, git, curl, coreutils, util-linux, tailscale).
- Never ask for, print or store the GitHub token. The user types it at a hidden prompt (README 3.3).
- Never change code, `deploy/update.sh` or the unit on this laptop. Anything that needs a change goes back to
  the Mac session: write it down and stop.
- nestje runs on this laptop too (service `nestje`, Funnel port 443). Never touch it. After the Funnel step,
  check that it still works.
- If anything is unclear or a command fails, stop and ask. Don't guess, and don't retry with other flags.

## Facts
- Release v0.1.0 is on GitHub (assets `leitner-linux` and `leitner-linux.sha256`), tag commit `c25170e`.
  If `update.sh` says 404 for the release, the user hasn't published it yet: stop and say so.
- Funnel name: the same one nestje uses. Find it with `tailscale status --json` (`Self.DNSName`, without the
  final dot) and use it wherever the README says `NAME.TAILNET.ts.net`. It must not end up in the repo.
- leitner listens on `127.0.0.1:8370`; Funnel serves it on port **8443**. Public URL: `https://<funnel name>:8443/`.
- The user still needs a fine-grained token (read-only Contents on `Damba017/leitner`), made on the Mac (README 3.3).
  Ask whether they have it before step 3.3.
- The laptop already stays on (system Wi-Fi, no sleep), set up for nestje. Nothing to do there.

## Steps
README section 3.1 is already done (this clone). Then, in order:

| README | Step | Expected |
| --- | --- | --- |
| 3.1 | `git -C ~/leitner config credential.helper store` | no output |
| 3.2 | System user | `id leitner` shows uid/gid `leitner` |
| 3.3 | Token file | `-rw------- root root`, then `200` |
| 3.4 | `cd ~/leitner/deploy && sudo bash update.sh` | checksum OK, ends with "No /etc/systemd/system/leitner.service yet"; `/opt/leitner/leitner version` → `leitner v0.1.0 (c25170e)` |
| 3.5 | Unit (with `sed` for the Funnel name) | `ExecStart` ends in `-url https://<funnel name>:8443`; `verify` silent; journal: `msg=listening addr=127.0.0.1:8370 … version="v0.1.0 (c25170e)"` |
| 3.6 | First admin (as `leitner`) | asks name, email, password twice |
| 3.7 | Funnel on 8443 | `tailscale funnel status` lists `:443` and `:8443`; `curl -sI https://<funnel name>:8443/` → `303`; port 443 still nestje |
| 4 | Checks: login on the iPhone (mobile data), client IP, login speed, nestje, backup, reboot | see README section 4 |

The most important check is the **client IP** (README 4): after one wrong password on the iPhone over mobile data,
`journalctl -u leitner -g 'login failed' -n 5 --no-pager` must show the phone's public IP, not `127.0.0.1`.

Out of scope for now: testing update and rollback (needs release v0.1.1, made later on the Mac), and README sections 7 and 8.

## At the end
Give the user a short report in Dutch to take back to the Mac session:
- output of `/opt/leitner/leitner version` and `systemctl is-active leitner`;
- the last line of `systemd-analyze security leitner.service` (the score);
- `tailscale funnel status`;
- the result of each check in README section 4, especially the `ip=` from the client-IP check;
- every deviation from this brief or the README, with the exact error message.
