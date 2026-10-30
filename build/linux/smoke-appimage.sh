#!/bin/sh
# Smoke-test the AppImage in a clean ubuntu:26.04 container without development
# packages: it must start, keep running for 15 seconds with a live WebKit web
# process, and exit with code 0 on SIGTERM.
#
# The AppImage relies on the host for libraries from the AppImage exclude list
# and for bubblewrap (WebKitGTK sandbox); every Ubuntu desktop has them, a bare
# container does not, so they are installed here. See docs/dev-notes.md.
set -eu
appimage="$1"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq >/dev/null
apt-get install -y -qq xvfb xauth dbus libegl1 libgles2 libfontconfig1 libfribidi0 libharfbuzz0b \
  bubblewrap xdg-dbus-proxy >/dev/null 2>&1
dbus-uuidgen >/etc/machine-id
# WebKit's bubblewrap sandbox needs an unprivileged user, as on a desktop.
useradd -m smoke
log=/home/smoke/app.log

runuser -u smoke -- sh -c "cd /home/smoke && exec xvfb-run -a dbus-run-session -- '$appimage' --appimage-extract-and-run >'$log' 2>&1" &
pid=$!
sleep 15

fail=0
if ! kill -0 "$pid" 2>/dev/null; then
  echo "smoke: application exited early"; fail=1
elif ! pgrep -f WebKitWebProcess >/dev/null; then
  echo "smoke: no WebKitWebProcess running"; fail=1
fi
if grep -Ei 'error while loading shared libraries|cannot open shared object|\*\* .*ERROR|SIGABRT|SIGSEGV|panic:|crashed' "$log"; then
  echo "smoke: errors in application log"; fail=1
fi

pkill -TERM -x smartnote || true
rc=0
wait "$pid" || rc=$?
echo "smoke: exit code $rc"
[ "$rc" -eq 0 ] || fail=1

if [ "$fail" -ne 0 ]; then
  echo "--- application log"; cat "$log"; exit 1
fi
echo "smoke: ok"
