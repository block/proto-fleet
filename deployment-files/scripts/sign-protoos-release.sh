#!/usr/bin/env bash
# Sign the release statement of a ProtoOS CT1 package with an AWS KMS key.
#
# Frigate's root update helper installs a proto-os package only when a release
# statement describing exactly that package verifies against a trusted ProtoOS
# release key. This script:
#
#   1. checks that the package's .sha256 sidecar is bound to the package,
#   2. verifies the package against the CT1 contract and writes its canonical
#      release statement (create-protoos-frigate-bundle.py --write-statement),
#   3. reads the KMS public key, requires ECDSA P-256 and reports its key ID
#      (sha256 of the PKIX DER public key, as Frigate computes it), and refuses
#      a key whose ID differs from an optional pin,
#   4. signs the statement with KMS (ECDSA_SHA_256 over the raw statement), and
#   5. verifies the signature with OpenSSL before anything is uploaded.
#
# It writes proto-os_<tag>_all.deb.statement.json and .statement.sig next to
# the package and never replaces existing files.
#
# Usage:
#   sign-protoos-release.sh --dir DIR --tag TAG --repository OWNER/NAME \
#       --commit SHA --kms-key-id KEY [--expected-key-id sha256:HEX] \
#       [--issued-at YYYY-MM-DDTHH:MM:SSZ]
#
# DIR must hold proto-os_<tag>_all.deb, its .sha256 sidecar and
# proto-os-<tag>.tar.gz. AWS credentials and region come from the environment.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUNDLE_SCRIPT="${SCRIPT_DIR}/create-protoos-frigate-bundle.py"
# Frigate's releasesig.MaxSignatureBytes.
MAX_SIGNATURE_BYTES=128

die() {
  echo "sign-protoos-release: $*" >&2
  exit 1
}

usage() {
  sed -n '20,23p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 2
}

dir="" tag="" repository="" commit="" kms_key_id="" expected_key_id="" issued_at=""
while (($# > 0)); do
  case "$1" in
    --dir | --tag | --repository | --commit | --kms-key-id | --expected-key-id | --issued-at)
      (($# >= 2)) || usage
      case "$1" in
        --dir) dir="$2" ;;
        --tag) tag="$2" ;;
        --repository) repository="$2" ;;
        --commit) commit="$2" ;;
        --kms-key-id) kms_key_id="$2" ;;
        --expected-key-id) expected_key_id="$2" ;;
        --issued-at) issued_at="$2" ;;
      esac
      shift 2
      ;;
    *) usage ;;
  esac
done
[[ -n "${dir}" && -n "${tag}" && -n "${repository}" && -n "${commit}" && -n "${kms_key_id}" ]] || usage
if [[ -n "${expected_key_id}" && ! "${expected_key_id}" =~ ^sha256:[0-9a-f]{64}$ ]]; then
  die "--expected-key-id must be sha256: followed by 64 lowercase hex digits"
fi

deb_name="proto-os_${tag}_all.deb"
deb="${dir}/${deb_name}"
archive="${dir}/proto-os-${tag}.tar.gz"
statement="${deb}.statement.json"
signature="${deb}.statement.sig"
for path in "${deb}" "${deb}.sha256" "${archive}"; do
  [[ -f "${path}" && ! -L "${path}" ]] || die "${path} is missing or not a regular file"
done
for path in "${statement}" "${signature}"; do
  [[ ! -e "${path}" && ! -L "${path}" ]] || die "${path} already exists"
done

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

# The sidecar is what operators compare by hand; refuse to sign a package it
# does not describe.
(cd "${dir}" && sha256sum "${deb_name}") > "${work}/expected.sha256"
cmp -s "${work}/expected.sha256" "${deb}.sha256" || die "${deb_name}.sha256 is not bound to ${deb_name}"

statement_args=(
  --repository "${repository}" --tag "${tag}" --commit "${commit}"
  --archive "${archive}" --verify-deb "${deb}" --write-statement "${work}/statement.json"
)
if [[ -n "${issued_at}" ]]; then
  statement_args+=(--issued-at "${issued_at}")
fi
python3 -I "${BUNDLE_SCRIPT}" "${statement_args[@]}" >&2

aws kms get-public-key --key-id "${kms_key_id}" --query PublicKey --output text \
  | base64 --decode > "${work}/kms-public-key.der"
openssl pkey -pubin -inform DER -in "${work}/kms-public-key.der" -out "${work}/public-key.pem" 2> /dev/null \
  || die "KMS returned a public key OpenSSL cannot parse"
key_text="$(openssl pkey -pubin -in "${work}/public-key.pem" -noout -text)"
grep -Eq 'ASN1 OID: prime256v1|NIST CURVE: P-256' <<< "${key_text}" \
  || die "the KMS key is not an ECDSA P-256 key"
# Hash the re-encoded PKIX DER so the key ID matches Frigate's releasesig.KeyID.
openssl pkey -pubin -in "${work}/public-key.pem" -outform DER -out "${work}/public-key.der"
key_id="sha256:$(openssl dgst -sha256 -r "${work}/public-key.der" | cut -d' ' -f1)"
if [[ -n "${expected_key_id}" && "${key_id}" != "${expected_key_id}" ]]; then
  die "the KMS key ID ${key_id} is not the expected ${expected_key_id}"
fi

aws kms sign --key-id "${kms_key_id}" --message-type RAW \
  --signing-algorithm ECDSA_SHA_256 --message "fileb://${work}/statement.json" \
  --query Signature --output text | base64 --decode > "${work}/statement.sig"
size="$(wc -c < "${work}/statement.sig" | tr -d ' ')"
((size > 0 && size <= MAX_SIGNATURE_BYTES)) \
  || die "the signature is ${size} bytes; Frigate accepts 1 to ${MAX_SIGNATURE_BYTES}"
openssl dgst -sha256 -verify "${work}/public-key.pem" \
  -signature "${work}/statement.sig" "${work}/statement.json" > /dev/null \
  || die "the KMS signature does not verify with the KMS public key"

set -o noclobber
cat "${work}/statement.json" > "${statement}"
cat "${work}/statement.sig" > "${signature}"
echo "Signed ${deb_name} with ProtoOS release key ${key_id}" >&2
echo "${key_id}"
