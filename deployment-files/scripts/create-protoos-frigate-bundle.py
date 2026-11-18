"""Stage and verify the ProtoOS bundle that CT1 Frigate controllers install.

Build mode copies a built ProtoOS tree into a fresh staging directory,
validates and hashes that copy, and writes the generated manifest
`.frigate-protoos-manifest.json` into it. The source tree is never changed, so
the release tarball and the R2 .ipk stay unchanged.

Verify mode opens a built .deb with the standard library only. It applies the
same closed contract as Frigate's root update helper: the Debian control data,
the absence of maintainer scripts, the allowed paths, the archive framing, the
size limits, and the manifest against the files in the package.

Frigate is the reference for every rule and for the manifest bytes. These
files in the Frigate repository define the rules:

* `internal/tool/stageprotoos/main.go` (bundle validation and manifest bytes).
* `cmd/frigate/protoos.go` (path segments and runtime limits).
* `internal/protoosdeb/` (the package contract and its size limits).

Keep this script and `tests/fixtures/protoos-frigate-bundle/` in step with them.
"""

import argparse
import hashlib
import json
import os
import re
import shutil
import stat
import sys
import tempfile
import zlib
from collections import namedtuple
from pathlib import Path

MANIFEST_NAME = ".frigate-protoos-manifest.json"
MANIFEST_SCHEMA_VERSION = 1
MAX_MANIFEST_BYTES = 4 << 20
PACKAGE_NAME = "proto-os"
VERSION_FILE_PATH = "etc/web_dashboard_version"
WEB_ROOT_PATH = "var/www"

REQUIRED_FILES = ("index.html", "config.js", "version.txt")
OPTIONAL_FILES = ("favicon.png",)
REQUIRED_DIRECTORIES = ("assets",)
OPTIONAL_DIRECTORIES = ("fonts",)

REPOSITORY_PATTERN = re.compile(
    r"[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*"
)
TAG_PATTERN = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]*")
COMMIT_PATTERN = re.compile(r"[0-9a-f]{40}")
# Full SemVer 2.0 grammar without build metadata.
_NUMERIC = r"(?:0|[1-9][0-9]*)"
_IDENTIFIER = r"(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)"
RELEASE_TAG_PATTERN = re.compile(
    rf"v({_NUMERIC})\.({_NUMERIC})\.({_NUMERIC})(?:-({_IDENTIFIER}(?:\.{_IDENTIFIER})*))?"
)
CONTROL_NAME_PATTERN = re.compile(r"[A-Za-z][A-Za-z0-9-]*")
MD5_LINE_PATTERN = re.compile(r"[0-9a-f]{32}  (.+)")

# Public bundle limits. The path limits leave room for the "var/www/" prefix
# inside the package, where Frigate's root helper allows 1024 bytes in total.
# The entry limit leaves room for the manifest and /etc/web_dashboard_version,
# which the helper counts in the 4096 data entries of the package.
Limits = namedtuple(
    "Limits",
    "max_archive_bytes max_file_bytes max_total_bytes max_entries"
    " max_path_bytes max_path_depth max_component_bytes",
)
DEFAULT_LIMITS = Limits(
    max_archive_bytes=32 << 20,
    max_file_bytes=32 << 20,
    max_total_bytes=128 << 20,
    max_entries=4096 - 2,
    max_path_bytes=1024 - len(WEB_ROOT_PATH + "/"),
    max_path_depth=32,
    max_component_bytes=255,
)

# Package limits, the same as in Frigate's internal/protoosdeb.
PackageLimits = namedtuple(
    "PackageLimits",
    "max_package_bytes max_control_bytes max_data_entries max_file_bytes"
    " max_total_bytes max_path_bytes max_component_bytes max_tar_padding",
)
DEFAULT_PACKAGE_LIMITS = PackageLimits(
    max_package_bytes=64 << 20,
    max_control_bytes=64 << 10,
    max_data_entries=4096,
    max_file_bytes=32 << 20,
    max_total_bytes=160 << 20,
    max_path_bytes=1024,
    max_component_bytes=255,
    max_tar_padding=1 << 20,
)
MAX_ENTRY_OVERHEAD = 4096

FILE_MODE = 0o644
DIRECTORY_MODE = 0o755

AR_MAGIC = b"!<arch>\n"
AR_HEADER_SIZE = 60
AR_MEMBERS = ("debian-binary", "control.tar.gz", "data.tar.gz")

EXPECTED_CONTROL = {
    "Package": PACKAGE_NAME,
    "Section": "web",
    "Priority": "optional",
    "Architecture": "all",
    "Maintainer": "Block Inc.",
    "Pre-Depends": "frigate-protoos-host (>= 1)",
    "Description": "ProtoOS web dashboard for CT1 Frigate controllers",
}


class BundleError(Exception):
    """A bundle or package violates the Frigate package contract."""


def sha256_file(path, limit):
    digest = hashlib.sha256()
    size = 0
    with open(path, "rb") as handle:
        for chunk in iter(lambda: handle.read(1 << 20), b""):
            size += len(chunk)
            if size > limit:
                raise BundleError(f"{path} is larger than {limit} bytes")
            digest.update(chunk)
    return digest.hexdigest(), size


def validate_path(relative, limits):
    """Apply Frigate's public path rules, plus its package path rules."""
    try:
        encoded = relative.encode("utf-8")
    except UnicodeEncodeError as error:
        raise BundleError(f"Path {relative!r} is not valid UTF-8") from error
    for character in relative:
        if ord(character) < 0x20 or ord(character) == 0x7F or character == "\\":
            raise BundleError(f"Path {relative!r} has a control character or backslash")
    if len(encoded) > limits.max_path_bytes:
        raise BundleError(f"Path {relative!r} is longer than the limit")
    segments = relative.split("/")
    if len(segments) > limits.max_path_depth:
        raise BundleError(f"Path {relative!r} is deeper than the limit")
    for segment in segments:
        if segment == "" or segment.startswith("."):
            raise BundleError(f"Path {relative!r} has invalid segment {segment!r}")
        if len(segment.encode("utf-8")) > limits.max_component_bytes:
            raise BundleError(f"Path {relative!r} has a segment that is too long")


def validate_release(repository, tag, commit):
    if not REPOSITORY_PATTERN.fullmatch(repository):
        raise BundleError(f"Invalid repository {repository!r}")
    if not TAG_PATTERN.fullmatch(tag):
        raise BundleError(f"Invalid tag {tag!r}")
    if not COMMIT_PATTERN.fullmatch(commit):
        raise BundleError(f"Invalid commit {commit!r}")
    debian_version(tag)


def archive_asset(tag):
    return f"proto-os-{tag}.tar.gz"


def collect_files(root, tag, commit, limits=DEFAULT_LIMITS):
    """Validate a public ProtoOS tree and return its sorted file records."""
    root = Path(root)
    if root.is_symlink() or not root.is_dir():
        raise BundleError(f"{root} is not a directory")
    for name in sorted(os.listdir(root)):
        validate_path(name, limits)
        mode = (root / name).lstat().st_mode
        if stat.S_ISLNK(mode):
            raise BundleError(f"{name} is a symbolic link")
        if name in REQUIRED_FILES or name in OPTIONAL_FILES:
            if not stat.S_ISREG(mode):
                raise BundleError(f"{name} is not a regular file")
        elif name in REQUIRED_DIRECTORIES or name in OPTIONAL_DIRECTORIES:
            if not stat.S_ISDIR(mode):
                raise BundleError(f"{name} is not a directory")
        else:
            raise BundleError(f"Unexpected top-level path {name!r}")
    for name in REQUIRED_FILES + REQUIRED_DIRECTORIES:
        if not (root / name).exists():
            raise BundleError(f"Missing required public path {name!r}")

    files = []
    entries = 0
    total = 0
    pending = [(root, "")]
    while pending:
        directory, prefix = pending.pop()
        for name in sorted(os.listdir(directory)):
            relative = prefix + name
            validate_path(relative, limits)
            path = directory / name
            mode = path.lstat().st_mode
            entries += 1
            if entries > limits.max_entries:
                raise BundleError("Too many public entries")
            if stat.S_ISLNK(mode):
                raise BundleError(f"{relative} is a symbolic link")
            if stat.S_ISDIR(mode):
                pending.append((path, relative + "/"))
            elif stat.S_ISREG(mode):
                digest, size = sha256_file(path, limits.max_file_bytes)
                total += size
                if total > limits.max_total_bytes:
                    raise BundleError("Public files are larger than the total limit")
                files.append({"path": relative, "size": size, "sha256": digest})
            else:
                raise BundleError(f"{relative} is not a regular file or directory")
    if not any(item["path"].startswith("assets/") for item in files):
        raise BundleError("The assets directory has no files")
    files.sort(key=lambda item: item["path"].encode("utf-8"))
    version_text = (root / "version.txt").read_bytes().decode("utf-8", "replace")
    if f"version: {tag}\n" not in version_text:
        raise BundleError("version.txt does not contain the release tag")
    if f"commit: {commit}\n" not in version_text:
        raise BundleError("version.txt does not contain the release commit")
    return files


def go_json(value, indent=None):
    """Encode like Go's json.Marshal (compact) or json.MarshalIndent.

    Go writes non-ASCII text as raw UTF-8 and escapes <, >, &, U+2028 and
    U+2029. Python already uses the same escapes for quotes, backslashes and
    control characters (lowercase hex), and bundle paths cannot hold the
    control characters that are the only difference.
    """
    if indent is None:
        text = json.dumps(value, separators=(",", ":"), ensure_ascii=False)
    else:
        text = json.dumps(value, indent=indent, ensure_ascii=False)
    for character, escaped in (
        ("<", "\\u003c"),
        (">", "\\u003e"),
        ("&", "\\u0026"),
        (" ", "\\u2028"),
        (" ", "\\u2029"),
    ):
        text = text.replace(character, escaped)
    return text.encode("utf-8")


def build_manifest(files, repository, tag, commit, archive_sha256):
    """Return the manifest bytes for validated file records."""
    release = {
        "repository": repository,
        "tag": tag,
        "commit": commit,
        "asset": archive_asset(tag),
        "sha256": archive_sha256,
    }
    generation_input = {
        "schema_version": MANIFEST_SCHEMA_VERSION,
        "release": release,
        "files": files,
    }
    manifest = {
        "schema_version": MANIFEST_SCHEMA_VERSION,
        "generation": hashlib.sha256(go_json(generation_input)).hexdigest(),
        "release": release,
        "files": files,
    }
    content = go_json(manifest, indent=2) + b"\n"
    if len(content) > MAX_MANIFEST_BYTES:
        raise BundleError(f"The manifest is larger than {MAX_MANIFEST_BYTES} bytes")
    return content


def archive_digest(archive, limits=DEFAULT_LIMITS):
    archive = Path(archive)
    if archive.is_symlink() or not archive.is_file():
        raise BundleError(f"{archive} is not a regular file")
    return sha256_file(archive, limits.max_archive_bytes)[0]


def copy_tree(source, output, limits):
    """Copy regular files and directories only; refuse anything else."""
    pending = [(source, output, "")]
    while pending:
        source_directory, output_directory, prefix = pending.pop()
        for name in sorted(os.listdir(source_directory)):
            relative = prefix + name
            validate_path(relative, limits)
            path = source_directory / name
            mode = path.lstat().st_mode
            if stat.S_ISLNK(mode):
                raise BundleError(f"{relative} is a symbolic link")
            if stat.S_ISDIR(mode):
                (output_directory / name).mkdir()
                pending.append((path, output_directory / name, relative + "/"))
            elif stat.S_ISREG(mode):
                shutil.copyfile(path, output_directory / name, follow_symlinks=False)
            else:
                raise BundleError(f"{relative} is not a regular file or directory")


def normalize_tree(root, epoch):
    """Set public modes and one fixed modification time on every entry."""
    for current, directories, names in os.walk(root, topdown=False):
        for name in names:
            path = Path(current) / name
            path.chmod(FILE_MODE)
            os.utime(path, (epoch, epoch))
        for name in directories:
            path = Path(current) / name
            path.chmod(DIRECTORY_MODE)
            os.utime(path, (epoch, epoch))
    Path(root).chmod(DIRECTORY_MODE)
    os.utime(root, (epoch, epoch))


def stage_bundle(
    source,
    output,
    repository,
    tag,
    commit,
    archive,
    epoch=0,
    limits=DEFAULT_LIMITS,
):
    """Copy a tree into a new directory, then validate and hash the copy."""
    validate_release(repository, tag, commit)
    source = Path(source)
    output = Path(output)
    if source.is_symlink() or not source.is_dir():
        raise BundleError(f"{source} is not a directory")
    archive_sha256 = archive_digest(archive, limits)
    if output.exists() or output.is_symlink():
        raise BundleError(f"{output} already exists")
    output.mkdir(parents=True)
    try:
        copy_tree(source, output, limits)
        files = collect_files(output, tag, commit, limits)
        manifest = build_manifest(files, repository, tag, commit, archive_sha256)
        manifest_path = output / MANIFEST_NAME
        manifest_path.write_bytes(manifest)
        normalize_tree(output, epoch)
    except BaseException:
        shutil.rmtree(output, ignore_errors=True)
        raise
    return manifest


def debian_version(tag):
    """Map a release tag to the Debian version that nfpm writes.

    The mapping is: remove the "v" and replace the first "-" with "~". Frigate
    and update tools must use the same mapping.
    """
    if not RELEASE_TAG_PATTERN.fullmatch(tag):
        raise BundleError(f"Tag {tag!r} is not a SemVer 2.0 release tag")
    version = tag[1:].replace("-", "~", 1)
    if not valid_debian_version(version):
        raise BundleError(f"Tag {tag!r} gives the invalid Debian version {version!r}")
    return version


def valid_debian_version(version):
    """Port of Frigate's ValidDebianVersion for versions without an epoch."""
    if not version or len(version) > 128 or ":" in version:
        return False
    upstream, hyphen, revision = version.rpartition("-")
    if not hyphen:
        upstream, revision = version, ""
    elif not revision or not re.fullmatch(r"[A-Za-z0-9.+~]+", revision):
        return False
    allowed = r"[A-Za-z0-9.+~-]+" if hyphen else r"[A-Za-z0-9.+~]+"
    return bool(re.fullmatch(allowed, upstream)) and upstream[:1].isdigit()


def gunzip_bounded(content, limit, what):
    """Decompress one gzip member, bounded in size, with nothing after it."""
    if content[:2] != b"\x1f\x8b":
        raise BundleError(f"{what} is not gzip data")
    decompressor = zlib.decompressobj(wbits=31)
    try:
        raw = decompressor.decompress(content, limit + 1)
    except zlib.error as error:
        raise BundleError(f"{what} is corrupt: {error}") from error
    if len(raw) > limit:
        raise BundleError(f"{what} expands beyond the allowed size")
    if not decompressor.eof:
        raise BundleError(f"{what} is truncated")
    if decompressor.unused_data:
        raise BundleError(f"{what} has trailing data after the gzip stream")
    return raw


def read_ar_members(data, max_bytes=DEFAULT_PACKAGE_LIMITS.max_package_bytes):
    """Return the three ar members after strict header checks."""
    if len(data) > max_bytes:
        raise BundleError("The package is larger than the allowed size")
    if not data.startswith(AR_MAGIC):
        raise BundleError("The package is not an ar archive")
    members = []
    offset = len(AR_MAGIC)
    for expected in AR_MEMBERS:
        header = data[offset : offset + AR_HEADER_SIZE]
        if len(header) != AR_HEADER_SIZE:
            raise BundleError(f"ar member {expected} is missing or truncated")
        if header[:16] != expected.encode().ljust(16):
            raise BundleError(f"Unexpected ar member {header[:16]!r}, want {expected}")
        if header[58:60] != b"`\n":
            raise BundleError(f"ar member {expected} has a bad header end")
        numbers = {}
        for field, (start, end) in {
            "time": (16, 28),
            "uid": (28, 34),
            "gid": (34, 40),
            "size": (48, 58),
        }.items():
            text = header[start:end].rstrip(b" ")
            if not text.isdigit() or len(text) > 12:
                raise BundleError(f"ar member {expected} has a bad {field}")
            numbers[field] = int(text)
        if numbers["uid"] != 0 or numbers["gid"] != 0:
            raise BundleError(f"ar member {expected} is not owned by root")
        if header[40:48] != b"100644  ":
            raise BundleError(f"ar member {expected} has an unexpected mode")
        offset += AR_HEADER_SIZE
        content = data[offset : offset + numbers["size"]]
        if len(content) != numbers["size"]:
            raise BundleError(f"ar member {expected} is truncated")
        members.append(content)
        offset += numbers["size"]
        if numbers["size"] % 2 and offset < len(data):
            if data[offset : offset + 1] != b"\n":
                raise BundleError(f"ar member {expected} has bad padding")
            offset += 1
    if offset != len(data):
        raise BundleError("The package has data after the last ar member")
    if members[0] != b"2.0\n":
        raise BundleError("Unsupported debian-binary version")
    return members


TarEntry = namedtuple(
    "TarEntry", "name type mode uid gid uname gname linkname devmajor devminor data"
)


def tar_octal(field, what):
    text = field.rstrip(b"\0 ").lstrip(b" ")
    if not text:
        return 0
    if not re.fullmatch(rb"[0-7]+", text):
        raise BundleError(f"tar field {what} is not octal")
    return int(text, 8)


def tar_text(field, what):
    try:
        return field.split(b"\0", 1)[0].decode("utf-8")
    except UnicodeDecodeError as error:
        raise BundleError(f"tar field {what} is not valid UTF-8") from error


def parse_tar(raw, what, max_padding):
    """Parse a ustar or GNU tar stream. Anything else is an error.

    The standard tarfile module hides the trailing slash of directory names
    and accepts link, device and PAX headers, so this reader is strict.
    """
    entries = []
    view = memoryview(raw)
    position = 0
    long_name = None
    while True:
        block = raw[position : position + 512]
        if len(block) < 512:
            raise BundleError(f"{what} has no tar end marker")
        if block == b"\0" * 512:
            if raw[position + 512 : position + 1024] != b"\0" * 512:
                raise BundleError(f"{what} has a bad tar end marker")
            padding = raw[position + 1024 :]
            if len(padding) > max_padding or any(padding):
                raise BundleError(f"{what} has data after the tar end marker")
            break
        checksum = sum(block[:148]) + 8 * 32 + sum(block[156:])
        if checksum != tar_octal(block[148:156], "checksum"):
            raise BundleError(f"{what} has a bad tar checksum")
        if block[257:262] != b"ustar":
            raise BundleError(f"{what} has a tar header that is not ustar or GNU")
        size = tar_octal(block[124:136], "size")
        start = position + 512
        end = start + size
        if end > len(raw):
            raise BundleError(f"{what} is truncated")
        position = start + (size + 511) // 512 * 512
        type_flag = block[156:157]
        if type_flag == b"L":
            if long_name is not None:
                raise BundleError(f"{what} has two GNU long names in a row")
            long_name = tar_text(raw[start:end], "long name")
            continue
        name = tar_text(block[0:100], "name")
        # Only POSIX ustar headers have a prefix field. In GNU headers these
        # bytes hold other fields, so do not read them as a path.
        if block[257:263] == b"ustar\x00":
            prefix = tar_text(block[345:500], "prefix")
            if prefix:
                name = prefix + "/" + name
        if long_name is not None:
            name, long_name = long_name, None
        entries.append(
            TarEntry(
                name=name,
                type=type_flag,
                mode=tar_octal(block[100:108], "mode"),
                uid=tar_octal(block[108:116], "uid"),
                gid=tar_octal(block[116:124], "gid"),
                uname=tar_text(block[265:297], "uname"),
                gname=tar_text(block[297:329], "gname"),
                linkname=tar_text(block[157:257], "linkname"),
                devmajor=tar_octal(block[329:337], "devmajor"),
                devminor=tar_octal(block[337:345], "devminor"),
                data=view[start:end],
            )
        )
    if long_name is not None:
        raise BundleError(f"{what} ends after a GNU long name")
    return entries


def canonical_relative(path, limits):
    """Port of Frigate's canonicalRel for package paths."""
    if path == "" or len(path.encode("utf-8")) > limits.max_path_bytes:
        raise BundleError("A package path is empty or too long")
    if path[0] == "/" or path[-1] == "/":
        raise BundleError(f"Package path {path!r} is absolute or ends with a slash")
    for character in path:
        if ord(character) < 0x20 or ord(character) == 0x7F or character == "\\":
            raise BundleError(f"Package path {path!r} has a control character")
    for component in path.split("/"):
        if component in ("", ".", ".."):
            raise BundleError(f"Package path {path!r} has an invalid component")
        if len(component.encode("utf-8")) > limits.max_component_bytes:
            raise BundleError(f"Package path {path!r} has a component that is too long")
    return path


def check_entry(entry, limits):
    """Port of Frigate's checkHeader. Return (relative path, is_directory)."""
    name = entry.name
    if entry.type not in (b"0", b"5"):
        raise BundleError(f"{name!r} is not a regular file or a directory")
    is_directory = entry.type == b"5"
    if entry.linkname or entry.devmajor or entry.devminor:
        raise BundleError(f"{name!r} has a link target or device numbers")
    if entry.uid != 0 or entry.gid != 0:
        raise BundleError(f"{name!r} is not owned by root")
    if entry.uname not in ("", "root") or entry.gname not in ("", "root"):
        raise BundleError(f"{name!r} has a non-root owner name")
    if is_directory:
        if entry.mode != DIRECTORY_MODE or len(entry.data):
            raise BundleError(f"Directory {name!r} must have mode 0755 and no data")
    elif entry.mode != FILE_MODE:
        raise BundleError(f"File {name!r} must have mode 0644")
    if not name.startswith("./"):
        raise BundleError(f"{name!r} is not in ./ form")
    relative = name[2:]
    if relative == "":
        if not is_directory or name != "./":
            raise BundleError(f"{name!r} is not a valid name")
        return "", True
    if is_directory != relative.endswith("/"):
        raise BundleError(f"{name!r} has a wrong trailing slash")
    return canonical_relative(
        relative.rstrip("/") if is_directory else relative, limits
    ), is_directory


def parse_control(text):
    if "\0" in text or "\r" in text or not text.endswith("\n"):
        raise BundleError("The control file is not a plain text file")
    fields = {}
    for line in text[:-1].split("\n"):
        if line.strip() == "" or line[0] in " \t":
            raise BundleError(f"Control line {line!r} is blank or a continuation")
        if any(ord(c) < 0x20 and c != "\t" or ord(c) == 0x7F for c in line):
            raise BundleError("The control file has a control character")
        key, separator, value = line.partition(":")
        if not separator or not CONTROL_NAME_PATTERN.fullmatch(key):
            raise BundleError(f"Control line {line!r} is not a field")
        if key in fields:
            raise BundleError(f"Duplicate control field {key}")
        fields[key] = value.strip(" \t")
    return fields


def verify_control(raw, tag, limits):
    entries = parse_tar(raw, "control.tar.gz", limits.max_tar_padding)
    members = {}
    root_seen = False
    for entry in entries:
        relative, is_directory = check_entry(entry, limits)
        if is_directory:
            if relative != "" or root_seen:
                raise BundleError("The control archive has a directory")
            root_seen = True
            continue
        if relative not in ("control", "md5sums"):
            raise BundleError(f"Control archive member {relative!r} is not allowed")
        if relative in members:
            raise BundleError(f"Duplicate control member {relative}")
        members[relative] = bytes(entry.data)
    if sorted(members) != ["control", "md5sums"]:
        raise BundleError("The control archive must hold control and md5sums")
    try:
        text = members["control"].decode("utf-8")
        md5sums = members["md5sums"].decode("utf-8")
    except UnicodeDecodeError as error:
        raise BundleError("A control file is not valid UTF-8") from error
    fields = parse_control(text)
    expected = dict(EXPECTED_CONTROL, Version=debian_version(tag))
    installed_size = fields.pop("Installed-Size", "0")
    if not installed_size.isdigit():
        raise BundleError("Installed-Size is not a number")
    if fields != expected:
        raise BundleError(f"Unexpected control fields: {fields}")
    return md5sums


def verify_data(raw, limits, destination):
    """Check every data entry, then write the web root files to destination."""
    entries = parse_tar(raw, "data.tar.gz", limits.max_tar_padding)
    explicit = set()
    files = set()
    directories = set()
    counted = 0
    total = 0
    contents = {}
    for entry in entries:
        relative, is_directory = check_entry(entry, limits)
        if relative == "":
            if "" in explicit:
                raise BundleError("The data archive has two root entries")
            explicit.add("")
            continue
        under_web_root = relative.startswith(WEB_ROOT_PATH + "/")
        if is_directory:
            fixed = relative in ("etc", "var", WEB_ROOT_PATH)
            if not fixed and not under_web_root:
                raise BundleError(f"Directory {relative!r} is not allowed")
        else:
            fixed = False
            if relative != VERSION_FILE_PATH and not under_web_root:
                raise BundleError(f"File {relative!r} is not allowed")
        if relative in explicit:
            raise BundleError(f"Duplicate data path {relative!r}")
        if not is_directory and relative in directories:
            raise BundleError(f"{relative!r} is a file and a directory")
        parts = relative.split("/")
        for index in range(1, len(parts)):
            parent = "/".join(parts[:index])
            if parent in files:
                raise BundleError(f"{relative!r} is below a regular file")
            directories.add(parent)
        explicit.add(relative)
        (directories if is_directory else files).add(relative)
        if not fixed:
            counted += 1
            if counted > limits.max_data_entries:
                raise BundleError("The data archive has too many entries")
        if not is_directory:
            if len(entry.data) > limits.max_file_bytes:
                raise BundleError(f"File {relative!r} is larger than the limit")
            total += len(entry.data)
            if total > limits.max_total_bytes:
                raise BundleError("The data archive is larger than the total limit")
            contents[relative] = entry.data
        if under_web_root:
            target = Path(destination) / relative
            if is_directory:
                target.mkdir(parents=True, exist_ok=True)
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(entry.data)
    for required in (VERSION_FILE_PATH, WEB_ROOT_PATH + "/" + MANIFEST_NAME):
        if required not in contents:
            raise BundleError(f"The package has no {required}")
    return contents


def verify_md5sums(text, contents, limits):
    listed = {}
    if text:
        if not text.endswith("\n"):
            raise BundleError("md5sums does not end with a newline")
        for line in text[:-1].split("\n"):
            match = MD5_LINE_PATTERN.fullmatch(line)
            if not match:
                raise BundleError(f"md5sums line {line!r} is malformed")
            # nfpm before 2.46 writes "./etc/x" and later versions write
            # "etc/x". Accept one optional leading "./" and store the canonical
            # path, as Frigate does.
            path = canonical_relative(match[1].removeprefix("./"), limits)
            if path in listed:
                raise BundleError(f"md5sums lists {path!r} twice")
            listed[path] = line[:32]
    expected = {
        path: hashlib.md5(content, usedforsecurity=False).hexdigest()
        for path, content in contents.items()
    }
    if listed != expected:
        raise BundleError("md5sums does not match the package files")


def verify_deb(
    deb,
    repository,
    tag,
    commit,
    archive,
    limits=DEFAULT_LIMITS,
    package_limits=DEFAULT_PACKAGE_LIMITS,
):
    """Raise BundleError unless the .deb matches the Frigate package contract."""
    validate_release(repository, tag, commit)
    deb = Path(deb)
    if deb.is_symlink() or not deb.is_file():
        raise BundleError(f"{deb} is not a regular file")
    if deb.stat().st_size > package_limits.max_package_bytes:
        raise BundleError("The package is larger than the allowed size")
    _, control, data = read_ar_members(
        deb.read_bytes(), package_limits.max_package_bytes
    )
    md5sums = verify_control(
        gunzip_bounded(control, package_limits.max_control_bytes, "control.tar.gz"),
        tag,
        package_limits,
    )
    budget = (
        package_limits.max_total_bytes
        + (package_limits.max_data_entries + 8) * MAX_ENTRY_OVERHEAD
        + package_limits.max_tar_padding
    )
    with tempfile.TemporaryDirectory() as directory:
        contents = verify_data(
            gunzip_bounded(data, budget, "data.tar.gz"), package_limits, directory
        )
        verify_md5sums(md5sums, contents, package_limits)
        if contents[VERSION_FILE_PATH] != f"{tag}\n".encode():
            raise BundleError(f"{VERSION_FILE_PATH} does not hold the tag")
        web_root = Path(directory) / WEB_ROOT_PATH
        embedded = (web_root / MANIFEST_NAME).read_bytes()
        (web_root / MANIFEST_NAME).unlink()
        files = collect_files(web_root, tag, commit, limits)
        expected = build_manifest(
            files, repository, tag, commit, archive_digest(archive, limits)
        )
        if embedded != expected:
            raise BundleError("The embedded manifest does not match the package files")


def parse_args(argv):
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("--repository", required=True, help="GitHub owner/name")
    parser.add_argument("--tag", required=True, help="Release tag")
    parser.add_argument("--commit", required=True, help="Full lowercase commit SHA")
    parser.add_argument("--archive", required=True, help="Release tar.gz to hash")
    parser.add_argument("--source", help="Built dist/protoOS directory (build mode)")
    parser.add_argument("--output", help="New staging directory (build mode)")
    parser.add_argument(
        "--source-date-epoch",
        type=int,
        default=os.environ.get("SOURCE_DATE_EPOCH"),
        help="Modification time of staged files (default: SOURCE_DATE_EPOCH)",
    )
    parser.add_argument("--verify-deb", help="Built .deb to verify (verify mode)")
    args = parser.parse_args(argv)
    if args.verify_deb:
        if args.source or args.output:
            parser.error("--verify-deb does not accept --source or --output")
    elif not (args.source and args.output):
        parser.error("--source and --output are required unless --verify-deb is set")
    elif args.source_date_epoch is None or args.source_date_epoch < 0:
        parser.error("--source-date-epoch or SOURCE_DATE_EPOCH is required")
    return args


def main(argv):
    args = parse_args(argv)
    try:
        if args.verify_deb:
            verify_deb(
                args.verify_deb, args.repository, args.tag, args.commit, args.archive
            )
            print(f"Verified {args.verify_deb}")
        else:
            stage_bundle(
                args.source,
                args.output,
                args.repository,
                args.tag,
                args.commit,
                args.archive,
                epoch=args.source_date_epoch,
            )
            print(f"Staged ProtoOS bundle in {args.output}")
    except (BundleError, OSError) as error:
        print(f"error: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
