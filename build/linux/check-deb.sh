#!/bin/sh
# Checks the contents of the .deb (specification 8.2 and 13): the application, the search
# worker and ONNX Runtime under /usr/local/lib/smartnote, the model files under
# /usr/local/share/smartnote/model and the licence notices under /usr/share/doc/smartnote.
# The worker must be executable; tokenizer.json is only for the tests and must not be shipped.
set -eu
deb="$1"
listing=$(dpkg-deb -c "$deb")

fail=0
check() {
  # $1: mode prefix of the dpkg-deb -c line, $2: path.
  if ! printf '%s\n' "$listing" | awk -v p=".$2" '$NF == p { print $1 }' | grep -q "^$1"; then
    echo "check-deb: $2 is missing or its mode is not $1"; fail=1
  fi
}

check -rwx /usr/local/bin/smartnote
check -rwxr-xr-x /usr/local/lib/smartnote/smartnote-search
check -rw-r--r-- /usr/local/lib/smartnote/libonnxruntime.so.1.31.0
for file in model.onnx tokenizer.kit manifest.json; do
  check -rw-r--r-- "/usr/local/share/smartnote/model/$file"
done
for file in THIRD_PARTY_NOTICES.md licenses/onnxruntime/LICENSE licenses/onnxruntime/ThirdPartyNotices.txt \
  licenses/rust-crates.txt licenses/go-modules.txt; do
  check -rw-r--r-- "/usr/share/doc/smartnote/$file"
done
if printf '%s\n' "$listing" | grep -q 'tokenizer\.json$'; then
  echo "check-deb: tokenizer.json is shipped"; fail=1
fi

if [ "$fail" -ne 0 ]; then
  printf '%s\n' "--- dpkg-deb -c $deb" "$listing"; exit 1
fi
echo "check-deb: ok"
