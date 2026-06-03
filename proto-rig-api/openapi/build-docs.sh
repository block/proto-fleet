#!/usr/bin/env bash
# Render MDK-API.json to a single self-contained, proto-fleet-branded HTML page
# under <site-dir>/proto-api-docs/, plus a root redirect so the Pages base URL
# (the deployment environment URL) lands on the docs instead of a 404.
# The proto-fleet logo and web fonts are injected at build time so the vendored
# spec (MDK-API.json) is never modified.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
spec="$here/MDK-API.json"
config="$here/redocly.yaml"
logo="$here/logo.svg"
site_dir="${1:?usage: build-docs.sh <site-dir>}"
subpath="proto-api-docs"
out="$site_dir/$subpath/index.html"
redocly_version="1.34.15"

mkdir -p "$(dirname "$out")"

# Embed the proto-fleet wordmark as a data URI and merge it into a temp copy of
# the spec via x-logo. The vendored spec is left untouched.
logo_b64="$(base64 <"$logo" | tr -d '\n')"
spec_with_logo="$here/.MDK-API.branded.json"
trap 'rm -f "$spec_with_logo"' EXIT
jq --arg url "data:image/svg+xml;base64,$logo_b64" \
  '.info["x-logo"] = {url: $url, altText: "Proto Fleet", href: "https://github.com/block/proto-fleet"}' \
  "$spec" >"$spec_with_logo"

npx -y "@redocly/cli@$redocly_version" build-docs "$spec_with_logo" --config "$config" -o "$out"

# Load Inter + JetBrains Mono (referenced by redocly.yaml typography) from Google
# Fonts so the docs match the app regardless of the viewer's installed fonts.
node -e '
const fs = require("fs");
const f = process.argv[1];
const links =
  "<link rel=\"preconnect\" href=\"https://fonts.googleapis.com\">" +
  "<link rel=\"preconnect\" href=\"https://fonts.gstatic.com\" crossorigin>" +
  "<link rel=\"stylesheet\" href=\"https://fonts.googleapis.com/css2?" +
  "family=Inter:ital,opsz,wght@0,14..32,100..900;1,14..32,100..900&" +
  "family=JetBrains+Mono:ital,wght@0,100..800;1,100..800&display=swap\">";
let html = fs.readFileSync(f, "utf8");
if (!html.includes(links)) html = html.replace("</head>", links + "</head>");
fs.writeFileSync(f, html);
' "$out"

# Redirect the Pages root to the docs. Relative target so it is domain-agnostic.
cat >"$site_dir/index.html" <<EOF
<!doctype html>
<meta charset="utf-8">
<title>Proto Fleet API Docs</title>
<meta http-equiv="refresh" content="0; url=$subpath/">
<link rel="canonical" href="$subpath/">
<a href="$subpath/">Proto Fleet API Docs</a>
EOF
