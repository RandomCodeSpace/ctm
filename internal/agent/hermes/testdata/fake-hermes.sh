#!/bin/bash
# Test shim for internal/agent/hermes/discover_test.go.
# Echoes $CTM_FAKE_HERMES_FIXTURE when called with `sessions list ...`.
# Any other invocation exits 99 to make accidental real calls obvious.
set -u

case "${1:-} ${2:-}" in
  "sessions list")
    if [ -n "${CTM_FAKE_HERMES_FIXTURE:-}" ] && [ -f "${CTM_FAKE_HERMES_FIXTURE}" ]; then
      cat "${CTM_FAKE_HERMES_FIXTURE}"
    fi
    exit 0
    ;;
  *)
    echo "fake-hermes: unsupported args $*" >&2
    exit 99
    ;;
esac
