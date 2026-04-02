#!/usr/bin/env bash
#
# pip-config.sh — Isolate pip from machine-global config during local setup.
#
# Source this script before running pip in repo-managed environments.
#
# Behavior:
#   1. If the caller already set PIP_CONFIG_FILE, honor it.
#   2. Otherwise, on local dev machines, point PIP_CONFIG_FILE at /dev/null
#      so machine-global pip.conf (e.g. custom index-url) is ignored.
#   3. In CI (GITHUB_ACTIONS or CI env var set), leave pip config untouched
#      so ambient CI configuration is honored.
#
# To override locally:
#   export PIP_CONFIG_FILE=~/.config/pip/pip.conf

if [[ -n "${PIP_CONFIG_FILE:-}" ]]; then
  export PIP_CONFIG_FILE
elif [[ -z "${CI:-}" && -z "${GITHUB_ACTIONS:-}" ]]; then
  export PIP_CONFIG_FILE=/dev/null
else
  unset PIP_CONFIG_FILE
fi
