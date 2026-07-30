#!/bin/sh
# Render runtime client configuration into config.js, read by the client as
# window.__RUNTIME_CONFIG__. Only keys that are set are emitted, so an unset key
# leaves the corresponding client provider a no-op. Runs on every container start
# (nginx executes /docker-entrypoint.d/*.sh before starting); config.js is served
# no-cache so operator changes take effect on restart without a rebuild.
set -eu

CONFIG_FILE="/usr/share/nginx/html/config.js"

printf 'window.__RUNTIME_CONFIG__ = {\n' > "$CONFIG_FILE"

emit_key() {
  key="$1"
  value="$2"
  [ -n "$value" ] || return 0
  # Strip CR/newlines (a raw newline would produce an unterminated JS string and
  # brick config.js), then escape backslashes and double quotes for safe embedding.
  escaped=$(printf '%s' "$value" | tr -d '\r\n' | sed 's/\\/\\\\/g; s/"/\\"/g')
  printf '  %s: "%s",\n' "$key" "$escaped" >> "$CONFIG_FILE"
}

# config.js is publicly served to browsers. Only emit public values (RUM
# application ID / client token are public identifiers). Never add a secret
# such as a Datadog API key (DD_API_KEY) to this list.

emit_key "DD_APPLICATION_ID" "${DD_APPLICATION_ID:-}"
emit_key "DD_CLIENT_TOKEN" "${DD_CLIENT_TOKEN:-}"
emit_key "DD_SITE" "${DD_SITE:-}"
emit_key "DD_SERVICE" "${DD_SERVICE:-}"
emit_key "DD_ENV" "${DD_ENV:-}"
emit_key "DD_RUM_SAMPLE_RATE" "${DD_RUM_SAMPLE_RATE:-}"
emit_key "DD_SESSION_REPLAY_SAMPLE_RATE" "${DD_SESSION_REPLAY_SAMPLE_RATE:-}"
emit_key "DD_TRACE_SAMPLE_RATE" "${DD_TRACE_SAMPLE_RATE:-}"

printf '};\n' >> "$CONFIG_FILE"
