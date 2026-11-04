#!/bin/sh
# Smoke-test the AppImage in a clean ubuntu:26.04 container without development
# packages: it must start, keep running for 15 seconds with a live WebKit web
# process, and exit with code 0 on SIGTERM. Then the search files of the AppImage
# are checked (see below) with searchsmoke, a static build of cmd/searchsmoke.
#
# Usage: smoke-appimage.sh APPIMAGE SEARCHSMOKE
#
# The AppImage relies on the host for libraries from the AppImage exclude list
# and for bubblewrap (WebKitGTK sandbox); every Ubuntu desktop has them, a bare
# container does not, so they are installed here. See docs/dev-notes.md.
set -eu
appimage="$1"
searchsmoke="${2:?usage: smoke-appimage.sh APPIMAGE SEARCHSMOKE}"
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
echo "smoke: application ok"

# The check above creates no storage, so the search worker does not start in it.
# The worker of the extracted AppImage runs with its ONNX Runtime and model from
# the working directory and with the library path that AppRun sets for the
# application, whose environment the worker inherits; semantic search must
# become ready, the search must find the note and the sandbox must be full.
runuser -u smoke -- sh -c "cd /home/smoke && '$appimage' --appimage-extract >/dev/null"
usr=/home/smoke/squashfs-root/usr
if ! runuser -u smoke -- sh -c "cd '$usr/lib' && LD_LIBRARY_PATH='$usr/lib:$usr/lib/x86_64-linux-gnu:$usr/lib/aarch64-linux-gnu:$usr/lib64' \
  exec '$searchsmoke' -worker '$usr/lib/smartnote/smartnote-search' -ort '$usr/lib/smartnote/libonnxruntime.so.1.31.0' \
  -model '$usr/share/smartnote/model' -timeout 60s"; then
  echo "smoke: search check failed"; exit 1
fi
echo "smoke: ok"
