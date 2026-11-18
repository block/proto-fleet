"""Check the ProtoOS bundle that CT1 Frigate controllers install.

Frigate's Go stager is the reference for the manifest bytes. The files in
`fixtures/protoos-frigate-bundle/` come from that stager:

* `proto-os-v0.6.2-internal.14.tar.gz` is a small fixture release archive. It
  holds file names with `[`, `&`, `<`, `>`, `"`, a space, `+`, `@`, an accent,
  U+2028, U+2029 and a non-BMP character, and names that test the sort order.
* `release.json` is the pin that Frigate's stager reads for that archive.
* `frigate-protoos-manifest.json` is the manifest that the stager wrote.

To refresh the golden manifest, run these commands with the fixture archive
(the pin holds its SHA-256, so update `sha256` in `release.json` if you
change the archive), where FRIGATE is a Frigate checkout:

    cd "$FRIGATE" && . bin/activate-hermit
    go run ./internal/tool/stageprotoos \
        -manifest <fixtures>/release.json \
        -archive <fixtures>/proto-os-v0.6.2-internal.14.tar.gz \
        -output build/protoos-golden
    cp build/protoos-golden/www/.frigate-protoos-manifest.json \
        <fixtures>/frigate-protoos-manifest.json

The archive root must be `dist/protoOS` and must have no `dist/` entry.

Frigate's `releasesig.MarshalCanonical` is the reference for the release
statement bytes. `proto-os_v0.6.2-internal.14_all.deb` is a small proto-os
package and `proto-os_v0.6.2-internal.14_all.deb.statement.json` is the
statement Frigate's release-statement tool wrote for it. To refresh it:

    cd "$FRIGATE" && . bin/activate-hermit
    cp <fixtures>/proto-os_v0.6.2-internal.14_all.deb build/
    go run ./cmd/frigate-release-statement create \
        --deb build/proto-os_v0.6.2-internal.14_all.deb \
        --repository btc-mining/proto-fleet-internal \
        --issued-at 2026-11-18T12:00:00Z
    cp build/proto-os_v0.6.2-internal.14_all.deb.statement.json <fixtures>/
"""

import gzip
import hashlib
import importlib.util
import io
import json
import os
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest
from pathlib import Path
from unittest import mock

TESTS = Path(__file__).resolve().parent
SCRIPT = TESTS.parent / "scripts/create-protoos-frigate-bundle.py"
CLIENT = TESTS.parents[1] / "client"
FIXTURES = TESTS / "fixtures/protoos-frigate-bundle"
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("protoos_frigate_bundle", SCRIPT)
bundle = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bundle)

REPOSITORY = "example-org/proto-fleet"
TAG = "v0.6.2-internal.14"
COMMIT = "0123456789abcdef0123456789abcdef01234567"
ARCHIVE = FIXTURES / f"proto-os-{TAG}.tar.gz"
EPOCH = 1700000000
# Release tags that exist in this repository, or that have the same shape.
TAG_CASES = (
    "v0.2.5-fw-auth-compat-2b0fcc4e7",
    "v0.2.0-alpha-ts-image-test-1",
    "v1.2.3-RC.1",
    "v0.6.2-internal.14",
    "v1.2.3",
)


def expected_debian_version(tag):
    return tag[1:].replace("-", "~", 1)


def extract_fixture(destination):
    with tarfile.open(ARCHIVE) as archive:
        archive.extractall(destination, filter="data")
    return Path(destination) / "dist/protoOS"


def make_tree(root, tag=TAG, commit=COMMIT):
    files = {
        "index.html": b"<html></html>\n",
        "config.js": b"window.config = {};\n",
        "version.txt": f"version: {tag}\ncommit: {commit}\n".encode(),
        "assets/app.js": b"app\n",
    }
    for name, content in files.items():
        path = Path(root) / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content)
    return Path(root)


def snapshot(root):
    result = {}
    for path in sorted(Path(root).rglob("*")):
        relative = str(path.relative_to(root))
        info = path.lstat()
        if path.is_symlink():
            result[relative] = ("link", os.readlink(path))
        elif path.is_dir():
            result[relative] = ("dir", info.st_mode, info.st_mtime)
        else:
            result[relative] = ("file", path.read_bytes(), info.st_mode, info.st_mtime)
    return result


def path_of_length(length, segment=200):
    """Return an `assets/...` path with exactly `length` bytes."""
    path = "assets"
    while len(path) + 1 + segment < length:
        path += "/" + "d" * segment
    return path + "/" + "f" * (length - len(path) - 1)


class TemporaryDirectoryCase(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.archive = self.root / f"proto-os-{TAG}.tar.gz"
        self.archive.write_bytes(b"archive")

    def stage(self, source, name="stage", **kwargs):
        return bundle.stage_bundle(
            source, self.root / name, REPOSITORY, TAG, COMMIT, self.archive, **kwargs
        )


class GoldenManifestTests(TemporaryDirectoryCase):
    def test_manifest_matches_frigate_go_stager(self):
        source = extract_fixture(self.root / "fixture")
        output = self.root / "stage"
        bundle.stage_bundle(source, output, REPOSITORY, TAG, COMMIT, ARCHIVE)
        golden = (FIXTURES / "frigate-protoos-manifest.json").read_bytes()
        self.assertEqual((output / bundle.MANIFEST_NAME).read_bytes(), golden)

    def test_golden_manifest_holds_the_special_names(self):
        paths = {
            item["path"]
            for item in json.loads(
                (FIXTURES / "frigate-protoos-manifest.json").read_text()
            )["files"]
        }
        for name in (
            "fonts/JetBrainsMono[wght].ttf",
            "assets/a&b.js",
            "assets/lt<gt>.js",
            "assets/café.js",
            "assets/with space.js",
            "assets/plus+at@.js",
            'assets/q"uote.js',
            "assets/sep .js",
            "assets/sep .js",
            "assets/emoji-\U0001f600.js",
        ):
            self.assertIn(name, paths)
        raw = (FIXTURES / "frigate-protoos-manifest.json").read_text(encoding="utf-8")
        self.assertIn("a\\u0026b.js", raw)
        self.assertIn("lt\\u003cgt\\u003e.js", raw)
        self.assertIn("sep\\u2028.js", raw)
        self.assertIn("café.js", raw)

    def test_release_pin_matches_fixture_archive(self):
        pin = json.loads((FIXTURES / "release.json").read_text())
        self.assertEqual(
            pin["sha256"], hashlib.sha256(ARCHIVE.read_bytes()).hexdigest()
        )
        self.assertEqual(pin["tag"], TAG)
        self.assertEqual(pin["commit"], COMMIT)
        self.assertEqual(pin["repository"], REPOSITORY)
        self.assertEqual(pin["asset"], ARCHIVE.name)

    def test_staged_tree_copies_files_with_public_modes(self):
        source = extract_fixture(self.root / "fixture")
        output = self.root / "stage"
        bundle.stage_bundle(source, output, REPOSITORY, TAG, COMMIT, ARCHIVE)
        for path in output.rglob("*"):
            expected = 0o755 if path.is_dir() else 0o644
            self.assertEqual(path.stat().st_mode & 0o7777, expected, path)
        for path in source.rglob("*"):
            if path.is_file():
                relative = path.relative_to(source)
                self.assertEqual((output / relative).read_bytes(), path.read_bytes())

    def test_manifest_shape_and_generation(self):
        manifest = (FIXTURES / "frigate-protoos-manifest.json").read_bytes()
        self.assertTrue(manifest.endswith(b"}\n"))
        data = json.loads(manifest)
        self.assertEqual(
            list(data), ["schema_version", "generation", "release", "files"]
        )
        self.assertEqual(
            list(data["release"]), ["repository", "tag", "commit", "asset", "sha256"]
        )
        paths = [item["path"] for item in data["files"]]
        self.assertEqual(paths, sorted(paths, key=lambda p: p.encode()))
        compact = bundle.go_json(
            {"schema_version": 1, "release": data["release"], "files": data["files"]}
        )
        self.assertEqual(data["generation"], hashlib.sha256(compact).hexdigest())

    def test_go_json_escapes(self):
        self.assertEqual(
            bundle.go_json({"a": '<>&"  é\\\n\t\x01'}),
            b'{"a":"\\u003c\\u003e\\u0026\\"\\u2028\\u2029\xc3\xa9\\\\\\n\\t\\u0001"}',
        )
        self.assertEqual(
            bundle.go_json({"a": [1]}, indent=2), b'{\n  "a": [\n    1\n  ]\n}'
        )


class RealClientTreeTests(TemporaryDirectoryCase):
    """A real release must not fail on the files in client/public."""

    def test_client_public_tree_is_accepted(self):
        tree = self.root / "dist"
        shutil.copytree(CLIENT / "public", tree)
        (tree / "index.html").write_text("<html></html>\n")
        (tree / "version.txt").write_text(f"version: {TAG}\ncommit: {COMMIT}\n")
        (tree / "assets").mkdir()
        (tree / "assets/app.js").write_text("app\n")
        manifest = json.loads(self.stage(tree))
        paths = {item["path"] for item in manifest["files"]}
        self.assertIn("fonts/JetBrainsMono/JetBrainsMono[wght].ttf", paths)
        self.assertIn("fonts/JetBrainsMono/JetBrainsMono-Italic[wght].ttf", paths)
        self.assertTrue(
            {
                path.relative_to(CLIENT / "public").as_posix()
                for path in (CLIENT / "public").rglob("*")
                if path.is_file()
            }
            <= paths
        )


class StageTests(TemporaryDirectoryCase):
    def test_output_is_deterministic(self):
        source = make_tree(self.root / "src")
        first = self.stage(source, "one", epoch=EPOCH)
        os.utime(source / "assets/app.js", (1, 1))
        second = self.stage(source, "two", epoch=EPOCH)
        self.assertEqual(first, second)
        self.assertEqual(snapshot(self.root / "one"), snapshot(self.root / "two"))

    def test_staged_mtimes_use_the_epoch(self):
        source = make_tree(self.root / "src")
        self.stage(source, epoch=EPOCH)
        for path in [self.root / "stage", *(self.root / "stage").rglob("*")]:
            self.assertEqual(path.lstat().st_mtime, EPOCH, path)

    def test_source_tree_is_not_modified(self):
        source = make_tree(self.root / "src")
        (source / "assets/empty").mkdir()
        before = snapshot(source)
        self.stage(source)
        self.assertEqual(snapshot(source), before)
        self.assertFalse((source / bundle.MANIFEST_NAME).exists())
        self.assertTrue((self.root / "stage/assets/empty").is_dir())

    def test_manifest_describes_the_staged_copy(self):
        source = make_tree(self.root / "src")
        original = shutil.copyfile

        def copy_and_change(src, dst, **kwargs):
            original(src, dst, **kwargs)
            if str(dst).endswith("app.js"):
                with open(dst, "ab") as handle:
                    handle.write(b"x")
            return dst

        with mock.patch.object(bundle.shutil, "copyfile", side_effect=copy_and_change):
            manifest = json.loads(self.stage(source))
        record = next(i for i in manifest["files"] if i["path"] == "assets/app.js")
        self.assertEqual(record["sha256"], hashlib.sha256(b"app\nx").hexdigest())

    def test_manifest_changes_with_content_and_archive(self):
        source = make_tree(self.root / "src")
        first = self.stage(source, "one")
        (source / "assets/app.js").write_bytes(b"changed\n")
        self.assertNotEqual(first, self.stage(source, "two"))
        self.archive.write_bytes(b"another archive")
        self.assertNotEqual(first, self.stage(source, "three"))

    def test_output_must_be_new(self):
        source = make_tree(self.root / "src")
        (self.root / "stage").mkdir()
        with self.assertRaises(bundle.BundleError):
            self.stage(source)

    def test_failed_stage_leaves_no_output(self):
        source = make_tree(self.root / "src")
        (source / "extra.txt").write_text("x")
        with self.assertRaises(bundle.BundleError):
            self.stage(source)
        self.assertFalse((self.root / "stage").exists())

    def test_archive_must_be_regular_and_small(self):
        source = make_tree(self.root / "src")
        with self.assertRaises(bundle.BundleError):
            bundle.stage_bundle(
                source, self.root / "a", REPOSITORY, TAG, COMMIT, self.root / "none"
            )
        small = bundle.DEFAULT_LIMITS._replace(max_archive_bytes=3)
        with self.assertRaises(bundle.BundleError):
            self.stage(source, "b", limits=small)

    def test_release_values_are_validated(self):
        source = make_tree(self.root / "src")
        for repository, tag, commit in (
            ("not-a-repository", TAG, COMMIT),
            (REPOSITORY, "bad tag", COMMIT),
            (REPOSITORY, "v1.2", COMMIT),
            (REPOSITORY, TAG, COMMIT.upper()),
            (REPOSITORY, TAG, COMMIT[:-1]),
        ):
            with (
                self.subTest(repository=repository, tag=tag, commit=commit),
                self.assertRaises(bundle.BundleError),
            ):
                bundle.stage_bundle(
                    source, self.root / "out", repository, tag, commit, self.archive
                )

    def test_source_must_not_be_a_symlink(self):
        source = make_tree(self.root / "src")
        (self.root / "link").symlink_to(source)
        with self.assertRaises(bundle.BundleError):
            self.stage(self.root / "link")


class ValidationTests(TemporaryDirectoryCase):
    def assert_rejected(self, source, **kwargs):
        with self.assertRaises(bundle.BundleError):
            self.stage(source, **kwargs)

    def test_unknown_top_level_file_and_directory(self):
        source = make_tree(self.root / "src")
        (source / "robots.txt").write_text("x")
        self.assert_rejected(source)
        (source / "robots.txt").unlink()
        (source / "other").mkdir()
        self.assert_rejected(source, name="other-stage")

    def test_hidden_or_empty_segments(self):
        source = make_tree(self.root / "src")
        hidden = source / "assets/.hidden"
        hidden.write_text("x")
        self.assert_rejected(source)
        hidden.unlink()
        (source / "assets/.dir").mkdir()
        (source / "assets/.dir/file.js").write_text("x")
        self.assert_rejected(source, name="dir-stage")

    def test_top_level_manifest_is_rejected(self):
        source = make_tree(self.root / "src")
        (source / bundle.MANIFEST_NAME).write_text("{}")
        self.assert_rejected(source)

    def test_symlinks(self):
        source = make_tree(self.root / "src")
        link = source / "assets/link.js"
        link.symlink_to("app.js")
        self.assert_rejected(source)
        link.unlink()
        (source / "assets/dir-link").symlink_to(".")
        self.assert_rejected(source, name="stage-2")
        (source / "assets/dir-link").unlink()
        (source / "favicon.png").symlink_to("index.html")
        self.assert_rejected(source, name="stage-3")

    def test_required_files_and_assets(self):
        for name in ("index.html", "config.js", "version.txt", "assets/app.js"):
            with self.subTest(name=name):
                source = make_tree(self.root / ("src-" + name.replace("/", "-")))
                (source / name).unlink()
                self.assert_rejected(source, name="stage-" + name.replace("/", "-"))
        source = make_tree(self.root / "empty-assets")
        (source / "assets/app.js").unlink()
        (source / "assets/sub").mkdir()
        self.assert_rejected(source, name="stage-empty")

    def test_assets_must_be_a_directory(self):
        source = make_tree(self.root / "src")
        shutil.rmtree(source / "assets")
        (source / "assets").write_text("x")
        self.assert_rejected(source)

    def test_version_text(self):
        source = make_tree(self.root / "src")
        cases = (
            f"version: other\ncommit: {COMMIT}\n",
            f"version: {TAG}\ncommit: {'a' * 40}\n",
            f"version: {TAG}\ncommit: {COMMIT}",
            f"version: {TAG}",
            "",
        )
        for index, text in enumerate(cases):
            with self.subTest(text=text):
                (source / "version.txt").write_text(text)
                self.assert_rejected(source, name=f"stage-{index}")

    def test_version_text_accepts_extra_lines(self):
        source = make_tree(self.root / "src")
        (source / "version.txt").write_text(
            f"version: {TAG}\nbuild_date: 2026-01-01T00:00:00Z\ncommit: {COMMIT}\n"
        )
        self.stage(source)

    def test_frigate_path_rules_allow_these_characters(self):
        names = (
            "a b.js",
            "a+b.js",
            "a@b.js",
            "a,b.js",
            "a<b>.js",
            "a&b.js",
            "x[wght].ttf",
            "café.js",
            "a b.js",
            "a-B_c.1.min.js",
            "emoji-\U0001f600.js",
        )
        source = make_tree(self.root / "src")
        for name in names:
            (source / "assets" / name).write_text("x")
        manifest = json.loads(self.stage(source))
        listed = {item["path"] for item in manifest["files"]}
        for name in names:
            self.assertIn("assets/" + name, listed)

    def test_control_characters_and_backslashes_are_rejected(self):
        for index, name in enumerate(
            ("a\x01.js", "a\x1f.js", "a\x7f.js", "a\nb.js", "a\\b.js")
        ):
            with self.subTest(name=name):
                source = make_tree(self.root / f"src-{index}")
                (source / "assets" / name).write_text("x")
                self.assert_rejected(source, name=f"stage-{index}")

    def test_invalid_utf8_names_are_rejected(self):
        with self.assertRaises(bundle.BundleError):
            bundle.validate_path("assets/\udcff.js", bundle.DEFAULT_LIMITS)

    def test_path_and_component_lengths(self):
        limits = bundle.DEFAULT_LIMITS
        self.assertEqual(limits.max_path_bytes, 1016)
        self.assertEqual(limits.max_component_bytes, 255)
        bundle.validate_path(path_of_length(1016), limits)
        for length in (1017, 4096):
            with self.assertRaises(bundle.BundleError):
                bundle.validate_path(path_of_length(length), limits)
        bundle.validate_path("assets/" + "a" * 255, limits)
        with self.assertRaises(bundle.BundleError):
            bundle.validate_path("assets/" + "a" * 256, limits)
        # A multi-byte character counts by its UTF-8 length.
        bundle.validate_path("assets/" + "é" * 127, limits)
        with self.assertRaises(bundle.BundleError):
            bundle.validate_path("assets/" + "é" * 128, limits)

    def test_a_long_path_is_staged(self):
        source = make_tree(self.root / "src")
        target = source / path_of_length(900)
        target.parent.mkdir(parents=True)
        target.write_text("x")
        self.stage(source)

    def test_limits(self):
        source = make_tree(self.root / "src")
        (source / "assets/sub/deeper").mkdir(parents=True)
        (source / "assets/sub/deeper/file.js").write_bytes(b"12345678")
        defaults = bundle.DEFAULT_LIMITS
        for index, limits in enumerate(
            (
                defaults._replace(max_file_bytes=7),
                defaults._replace(max_total_bytes=20),
                defaults._replace(max_entries=5),
                defaults._replace(max_path_bytes=20),
                defaults._replace(max_path_depth=3),
                defaults._replace(max_component_bytes=6),
            )
        ):
            with self.subTest(limits=limits):
                self.assert_rejected(source, name=f"stage-{index}", limits=limits)
        self.stage(source, name="stage-ok")

    def test_default_limits_match_frigate(self):
        self.assertEqual(
            bundle.DEFAULT_LIMITS,
            (32 << 20, 32 << 20, 128 << 20, 4096 - 2, 1016, 32, 255),
        )
        self.assertEqual(
            bundle.DEFAULT_PACKAGE_LIMITS,
            (64 << 20, 64 << 10, 4096, 32 << 20, 160 << 20, 1024, 255, 1 << 20),
        )

    def test_special_files(self):
        source = make_tree(self.root / "src")
        os.mkfifo(source / "assets/pipe")
        self.assert_rejected(source)


class TagTests(unittest.TestCase):
    def test_debian_version_maps_the_first_hyphen(self):
        for tag in TAG_CASES + ("v0.1.0-rc.1", "v10.20.30-a-b-c.d-e"):
            with self.subTest(tag=tag):
                self.assertEqual(
                    bundle.debian_version(tag), expected_debian_version(tag)
                )
        self.assertEqual(bundle.debian_version(TAG), "0.6.2~internal.14")

    def test_invalid_tags(self):
        for tag in (
            "nightly-20261118-abcdef012345",
            "0.6.2",
            "v1.0",
            "v1.2.3+build",
            "v1.2.3-rc.1+build",
            "v01.2.3",
            "v1.02.3",
            "v1.2.03",
            "v1.2.3-01",
            "v1.2.3-rc.01",
            "v1.2.3-a_b",
            "v1.2.3-",
            "v1.2.3-a..b",
            "v1.2.3-a.",
            "v1.2.3-a-",
        ):
            with self.subTest(tag=tag), self.assertRaises(bundle.BundleError):
                bundle.debian_version(tag)

    def test_workflow_tag_rules_match(self):
        """release.yml and the script use the same SemVer grammar."""
        text = (TESTS.parents[1] / ".github/workflows/release.yml").read_text()
        for fragment in (
            "numeric='(0|[1-9][0-9]*)'",
            "identifier='(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)'",
        ):
            self.assertIn(fragment, text)


def tar_gz(members):
    """Return gzip tar bytes from (name, content, options) tuples."""
    raw = io.BytesIO()
    has_pax = any(options.get("pax_headers") for _, _, options in members)
    with tarfile.open(
        fileobj=raw,
        mode="w",
        format=tarfile.PAX_FORMAT if has_pax else tarfile.GNU_FORMAT,
        encoding="utf-8",
    ) as tar:
        for name, content, options in members:
            info = tarfile.TarInfo(name)
            info.uid = options.get("uid", 0)
            info.gid = 0
            info.uname = info.gname = options.get("uname", "root")
            info.mode = options.get("mode", 0o644 if content is not None else 0o755)
            info.type = options.get(
                "type", tarfile.REGTYPE if content is not None else tarfile.DIRTYPE
            )
            info.pax_headers = options.get("pax_headers", {})
            if "linkname" in options:
                info.linkname = options["linkname"]
            data = content or b""
            if info.type in (tarfile.REGTYPE, tarfile.CONTTYPE):
                info.size = len(data)
            tar.addfile(info, io.BytesIO(data) if info.size else None)
    return gzip.compress(raw.getvalue(), mtime=0)


def ar_archive(members, mode="100644", owner=0, suffix=""):
    data = b"!<arch>\n"
    for name, content in members:
        header = (
            f"{name + suffix:<16}{0:<12}{owner:<6}{0:<6}{mode:<8}{len(content):<10}`\n"
        )
        data += header.encode() + content
        if len(content) % 2:
            data += b"\n"
    return data


def control_text(tag=TAG, **overrides):
    fields = {
        "Package": "proto-os",
        "Version": bundle.debian_version(tag),
        "Section": "web",
        "Priority": "optional",
        "Architecture": "all",
        "Maintainer": "Block Inc.",
        "Installed-Size": "1",
        "Pre-Depends": "frigate-protoos-host (>= 1)",
        "Description": "ProtoOS web dashboard for CT1 Frigate controllers",
    }
    fields.update(overrides)
    return "".join(f"{k}: {v}\n" for k, v in fields.items() if v is not None)


class DebBuilder:
    """Build proto-os packages from a staged tree in a TemporaryDirectoryCase."""

    def setUp(self):
        super().setUp()
        self.source = make_tree(self.root / "src")
        self.manifest = self.stage(self.source)
        self.staged = self.root / "stage"

    def data_members(self):
        members = [("./etc/", None, {}), ("./var/", None, {}), ("./var/www/", None, {})]
        for path in sorted(self.staged.rglob("*")):
            relative = path.relative_to(self.staged).as_posix()
            if path.is_dir():
                members.append((f"./var/www/{relative}/", None, {}))
            else:
                members.append((f"./var/www/{relative}", path.read_bytes(), {}))
        members.append(("./etc/web_dashboard_version", f"{TAG}\n".encode(), {}))
        return members

    def build(
        self, data=None, control=None, control_members=None, parts=None, md5=None
    ):
        data = self.data_members() if data is None else data
        files = {
            name.removeprefix("./"): content
            for name, content, options in data
            if content is not None and options.get("type") in (None, tarfile.REGTYPE)
        }
        md5sums = md5
        if md5sums is None:
            md5sums = "".join(
                f"{hashlib.md5(content).hexdigest()}  {name}\n"
                for name, content in sorted(files.items())
            )
        members = control_members or [
            ("./control", (control or control_text()).encode(), {}),
            ("./md5sums", md5sums.encode(), {}),
        ]
        parts = parts or [
            ("debian-binary", b"2.0\n"),
            ("control.tar.gz", tar_gz(members)),
            ("data.tar.gz", tar_gz(data)),
        ]
        path = self.root / "proto-os.deb"
        path.write_bytes(ar_archive(parts))
        return path


class DebVerificationTests(DebBuilder, TemporaryDirectoryCase):
    def verify(self, deb, **kwargs):
        bundle.verify_deb(deb, REPOSITORY, TAG, COMMIT, self.archive, **kwargs)

    def assert_rejected(self, deb, **kwargs):
        with self.assertRaises(bundle.BundleError):
            self.verify(deb, **kwargs)

    def replaced(self, name, content=None, **options):
        members = [m for m in self.data_members() if m[0] != name]
        members.append((name, content, options))
        return members

    def test_valid_package(self):
        self.verify(self.build())

    def test_control_fields(self):
        for overrides in (
            {"Package": "other"},
            {"Version": "0.6.2-internal.14"},
            {"Architecture": "amd64"},
            {"Maintainer": "Someone"},
            {"Pre-Depends": None},
            {"Pre-Depends": "frigate-protoos-host (>= 2)"},
            {"Depends": "libc6"},
            {"Recommends": "x"},
            {"Provides": "x"},
            {"Conflicts": "x"},
            {"Replaces": "x"},
            {"Breaks": "x"},
            {"Suggests": "x"},
            {"Section": "admin"},
            {"Priority": "required"},
            {"Description": "Other"},
            {"Installed-Size": "many"},
        ):
            with self.subTest(overrides=overrides):
                self.assert_rejected(self.build(control=control_text(**overrides)))

    def test_control_text_forms(self):
        good = control_text()
        for text in (
            good.rstrip("\n"),
            good + "\n",
            good.replace("\n", "\r\n"),
            " " + good,
        ):
            with self.subTest(text=text):
                self.assert_rejected(self.build(control=text))

    def valid_control_members(self):
        """Return the control and md5sums members of a package that verifies."""
        return [
            ("./control", control_text().encode(), {}),
            ("./md5sums", self.md5_lines().encode(), {}),
        ]

    def test_maintainer_scripts_and_extra_control_members(self):
        # The extra member has mode 0644, and md5sums agrees with the data.
        # Thus only the list of permitted member names can reject the package.
        members = self.valid_control_members()
        self.verify(self.build(control_members=members))
        for name in ("preinst", "postinst", "prerm", "postrm", "triggers", "conffiles"):
            with self.subTest(name=name):
                extra = (f"./{name}", b"#!/bin/sh\n", {})
                expected = f"Control archive member '{name}' is not allowed"
                with self.assertRaisesRegex(bundle.BundleError, expected):
                    self.verify(self.build(control_members=[*members, extra]))

    def test_executable_maintainer_script(self):
        # A real maintainer script is executable. The mode rule rejects it
        # before the verifier examines the member name.
        script = ("./postinst", b"#!/bin/sh\n", {"mode": 0o755})
        members = [*self.valid_control_members(), script]
        with self.assertRaisesRegex(bundle.BundleError, "must have mode 0644"):
            self.verify(self.build(control_members=members))

    def test_control_member_modes_owners_and_names(self):
        md5 = ""
        for index, (name, options) in enumerate(
            (
                ("./control", {"mode": 0o600}),
                ("./control", {"mode": 0o755}),
                ("./control", {"uid": 1000}),
                ("./control", {"uname": "nobody"}),
                ("control", {}),
                ("./control", {"type": tarfile.CONTTYPE}),
            )
        ):
            with self.subTest(name=name, options=options):
                members = [
                    (name, control_text().encode(), options),
                    ("./md5sums", md5.encode(), {}),
                ]
                self.assert_rejected(self.build(control_members=members, md5=md5))
        members = [("./control", control_text().encode(), {}), ("./control", b"x", {})]
        self.assert_rejected(self.build(control_members=members))

    def test_ar_framing(self):
        good = [
            ("debian-binary", b"2.0\n"),
            ("control.tar.gz", tar_gz([])),
            ("data.tar.gz", tar_gz([])),
        ]
        self.assert_rejected(self.build(parts=[good[1], good[0], good[2]]))
        self.assert_rejected(self.build(parts=good[:2]))
        self.assert_rejected(self.build(parts=good + [("extra", b"")]))
        self.assert_rejected(self.build(parts=good[:1] + [("control.tar.xz", b"")]))
        self.assert_rejected(self.build(parts=[("debian-binary", b"2.1\n")] + good[1:]))
        deb = self.build()
        names = ["debian-binary", "control.tar.gz", "data.tar.gz"]
        members = list(zip(names, bundle.read_ar_members(deb.read_bytes())))
        for kwargs in ({"suffix": "/"}, {"mode": "100755"}, {"owner": 1000}):
            with self.subTest(kwargs=kwargs):
                deb.write_bytes(ar_archive(members, **kwargs))
                self.assert_rejected(deb)
        deb.write_bytes(ar_archive(members) + b"trailing")
        self.assert_rejected(deb)
        deb.write_bytes(b"not an archive")
        self.assert_rejected(deb)

    def test_compression_must_be_one_gzip_member(self):
        deb = self.build()
        names = ["debian-binary", "control.tar.gz", "data.tar.gz"]
        members = bundle.read_ar_members(deb.read_bytes())
        raw = gzip.decompress(members[2])
        for data in (
            raw,
            members[2] + b"junk",
            members[2] + members[2],
            members[2][:-8],
            gzip.compress(raw + b"x" * 10)[:-1] + b"\0",
        ):
            with self.subTest(size=len(data)):
                deb.write_bytes(
                    ar_archive(list(zip(names, [members[0], members[1], data])))
                )
                self.assert_rejected(deb)
        deb.write_bytes(
            ar_archive(list(zip(names, [members[0], members[1][:-3], members[2]])))
        )
        self.assert_rejected(deb)

    def test_data_paths(self):
        for name in (
            "./usr/bin/tool",
            "./etc/other",
            "./var/lib/file",
            "./var/www/../escape",
            "./var/www/./index2.html",
            "./var/www//double.js",
            "./var/www/assets/.hidden",
            "./var/www/unknown.txt",
            "./var/www/assets/bad\x01name.js",
            "./var/www/assets/back\\slash.js",
            "./var/www",
            "var/www/assets/new.js",
            "/var/www/assets/new.js",
        ):
            with self.subTest(name=name):
                self.assert_rejected(
                    self.build(data=self.data_members() + [(name, b"x", {})])
                )

    def test_data_directory_forms(self):
        base = self.data_members()
        self.assert_rejected(self.build(data=base + [("./usr/", None, {})]))
        self.assert_rejected(self.build(data=base + [("./var/www/x/", b"x", {})]))
        self.assert_rejected(self.build(data=base + [("./var/www/assets", b"", {})]))
        self.assert_rejected(self.build(data=base + [("./var/www/assets/", None, {})]))
        self.assert_rejected(
            self.build(data=base + [("./var/www/index.html/below", b"x", {})])
        )

    def test_data_types_modes_and_owners(self):
        base = self.data_members()
        cases = {
            "symlink": base
            + [
                (
                    "./var/www/assets/l",
                    None,
                    {"type": tarfile.SYMTYPE, "linkname": "app.js"},
                )
            ],
            "hardlink": base
            + [
                (
                    "./var/www/assets/h",
                    None,
                    {"type": tarfile.LNKTYPE, "linkname": "./var/www/index.html"},
                )
            ],
            "device": base + [("./var/www/assets/d", None, {"type": tarfile.CHRTYPE})],
            "fifo": base + [("./var/www/assets/p", None, {"type": tarfile.FIFOTYPE})],
            "contiguous": self.replaced(
                "./var/www/index.html", b"<html></html>\n", type=tarfile.CONTTYPE
            ),
            "pax": self.replaced(
                "./var/www/index.html", b"<html></html>\n", pax_headers={"comment": "x"}
            ),
            "setuid": self.replaced(
                "./var/www/index.html", b"<html></html>\n", mode=0o4755
            ),
            "group-write": self.replaced(
                "./var/www/index.html", b"<html></html>\n", mode=0o664
            ),
            "executable": self.replaced(
                "./var/www/index.html", b"<html></html>\n", mode=0o755
            ),
            "directory-mode": base + [("./var/www/assets/sub/", None, {"mode": 0o700})],
            "owner": self.replaced(
                "./var/www/index.html", b"<html></html>\n", uid=1000
            ),
            "owner-name": self.replaced(
                "./var/www/index.html", b"<html></html>\n", uname="nobody"
            ),
            "version-file-mode": self.replaced(
                "./etc/web_dashboard_version", f"{TAG}\n".encode(), mode=0o755
            ),
        }
        for name, data in cases.items():
            with self.subTest(case=name):
                self.assert_rejected(self.build(data=data))

    def test_duplicate_data_path(self):
        data = self.data_members() + [("./var/www/index.html", b"<html></html>\n", {})]
        self.assert_rejected(self.build(data=data))

    def test_version_file_content(self):
        data = self.replaced("./etc/web_dashboard_version", f"{TAG}".encode())
        self.assert_rejected(self.build(data=data))

    def test_manifest_mismatches(self):
        data = self.replaced("./var/www/assets/app.js", b"tampered\n")
        self.assert_rejected(self.build(data=data))
        extra = self.data_members() + [("./var/www/assets/new.js", b"x", {})]
        self.assert_rejected(self.build(data=extra))
        missing = [
            m
            for m in self.data_members()
            if m[0] != "./var/www/.frigate-protoos-manifest.json"
        ]
        self.assert_rejected(self.build(data=missing))

    def md5_lines(self, prefix="", extra=()):
        control = bundle.read_ar_members(self.build().read_bytes())[1]
        raw = bundle.gunzip_bounded(control, 1 << 16, "control")
        entries = bundle.parse_tar(raw, "control", 1 << 20)
        text = next(e for e in entries if e.name == "./md5sums").data.tobytes().decode()
        lines = [prefix + line for line in text.split("\n") if line]
        return "".join(line + "\n" for line in [*lines, *extra])

    def test_md5sums_accept_one_optional_dot_slash(self):
        plain = self.md5_lines()
        self.assertIn("  etc/web_dashboard_version\n", plain)
        self.verify(self.build(md5=plain))
        dotted = "".join(
            line[:34] + "./" + line[34:] + "\n" for line in plain.split("\n") if line
        )
        self.assertIn("  ./etc/web_dashboard_version\n", dotted)
        self.verify(self.build(md5=dotted))

    def test_md5sums_rejections(self):
        plain = self.md5_lines()
        first = plain.split("\n")[0]
        wrong = "0" * 32 + "  var/www/index.html\n"
        double = first[:34] + "././" + first[34:] + "\n"
        same_twice = plain + first[:34] + "./" + first[34:] + "\n"
        listed_twice = plain + first + "\n"
        for md5 in (
            wrong,
            double,
            same_twice,
            listed_twice,
            "x\n",
            "abc",
            "0" * 32 + " var/www/index.html\n",
            first[:34] + "/" + first[34:] + "\n",
            first[:34] + ".//" + first[34:] + "\n",
        ):
            with self.subTest(md5=md5):
                self.assert_rejected(self.build(md5=md5))

    def test_long_names_use_gnu_long_names(self):
        source = make_tree(self.root / "long-src")
        target = source / path_of_length(900)
        target.parent.mkdir(parents=True)
        target.write_text("x")
        bundle.stage_bundle(
            source, self.root / "long", REPOSITORY, TAG, COMMIT, self.archive
        )
        self.staged = self.root / "long"
        self.verify(self.build())

    def test_package_limits(self):
        deb = self.build()
        defaults = bundle.DEFAULT_PACKAGE_LIMITS
        for limits in (
            defaults._replace(max_package_bytes=100),
            defaults._replace(max_control_bytes=100),
            defaults._replace(max_data_entries=2),
            defaults._replace(max_file_bytes=10),
            defaults._replace(max_total_bytes=10),
            defaults._replace(max_path_bytes=20),
            defaults._replace(max_component_bytes=8),
        ):
            with self.subTest(limits=limits):
                self.assert_rejected(deb, package_limits=limits)
        self.verify(deb)

    def test_entry_count_includes_the_manifest(self):
        deb = self.build()
        # etc, var and var/www are fixed. Everything else counts, including the
        # version file and the manifest.
        counted = sum(
            1
            for name, _, _ in self.data_members()
            if name not in ("./etc/", "./var/", "./var/www/")
        )
        defaults = bundle.DEFAULT_PACKAGE_LIMITS
        self.verify(deb, package_limits=defaults._replace(max_data_entries=counted))
        self.assert_rejected(
            deb, package_limits=defaults._replace(max_data_entries=counted - 1)
        )

    def test_release_arguments_are_bound(self):
        deb = self.build()
        with self.assertRaises(bundle.BundleError):
            bundle.verify_deb(deb, "other-org/proto-fleet", TAG, COMMIT, self.archive)
        with self.assertRaises(bundle.BundleError):
            bundle.verify_deb(deb, REPOSITORY, TAG, "f" * 40, self.archive)
        with self.assertRaises(bundle.BundleError):
            bundle.verify_deb(deb, REPOSITORY, "v9.9.9", COMMIT, self.archive)
        self.archive.write_bytes(b"other archive")
        self.assert_rejected(deb)


@unittest.skipUnless(
    shutil.which("nfpm") or os.environ.get("CI"),
    "nfpm is not on PATH",
)
class NfpmPackageTests(TemporaryDirectoryCase):
    """Build the real package from the fixture with the pinned nfpm."""

    def build_package(self, tag, name="client"):
        source = extract_fixture(self.root / f"fixture-{name}")
        (source / "version.txt").write_text(f"version: {tag}\ncommit: {COMMIT}\n")
        archive = self.root / f"proto-os-{tag}.tar.gz"
        archive.write_bytes(b"archive for " + tag.encode())
        work = self.root / name
        (work / "dist").mkdir(parents=True)
        shutil.copy(CLIENT / "nfpm-proto-os-deb.yaml", work)
        bundle.stage_bundle(
            source,
            work / "dist/protoOS-frigate",
            REPOSITORY,
            tag,
            COMMIT,
            archive,
            epoch=EPOCH,
        )
        (work / "dist/web_dashboard_version").write_text(tag + "\n")
        deb = work / f"proto-os_{tag}_all.deb"
        subprocess.run(
            ["nfpm", "package", "--config", "nfpm-proto-os-deb.yaml"]
            + ["--packager", "deb", "--target", deb.name],
            cwd=work,
            env=dict(os.environ, VERSION=tag, SOURCE_DATE_EPOCH=str(EPOCH)),
            check=True,
            capture_output=True,
        )
        return deb, archive

    def control_version(self, deb):
        control = bundle.gunzip_bounded(
            bundle.read_ar_members(deb.read_bytes())[1], 1 << 16, "control"
        )
        entries = bundle.parse_tar(control, "control", 1 << 20)
        text = next(e for e in entries if e.name == "./control").data.tobytes().decode()
        return next(
            line.split(": ", 1)[1]
            for line in text.split("\n")
            if line.startswith("Version:")
        )

    def test_package_meets_the_frigate_contract_for_each_tag(self):
        for tag in TAG_CASES:
            with self.subTest(tag=tag):
                deb, archive = self.build_package(tag, name="build-" + tag)
                bundle.verify_deb(deb, REPOSITORY, tag, COMMIT, archive)
                version = self.control_version(deb)
                self.assertEqual(version, expected_debian_version(tag))
                self.assertEqual(version, bundle.debian_version(tag))

    def test_builds_are_reproducible(self):
        first, _ = self.build_package(TAG, name="first")
        second, _ = self.build_package(TAG, name="second")
        self.assertEqual(first.read_bytes(), second.read_bytes())

    def test_package_with_the_special_names(self):
        deb, _ = self.build_package(TAG)
        members = bundle.read_ar_members(deb.read_bytes())
        data = bundle.gunzip_bounded(members[2], 1 << 26, "data")
        names = {e.name for e in bundle.parse_tar(data, "data", 1 << 20)}
        self.assertIn("./var/www/fonts/JetBrainsMono[wght].ttf", names)
        self.assertIn("./var/www/assets/sep .js", names)


class CommandLineTests(TemporaryDirectoryCase):
    def run_script(self, *args, epoch="1"):
        env = {k: v for k, v in os.environ.items() if k != "SOURCE_DATE_EPOCH"}
        if epoch is not None:
            env["SOURCE_DATE_EPOCH"] = epoch
        return subprocess.run(
            [sys.executable, "-I", str(SCRIPT), *args],
            capture_output=True,
            text=True,
            check=False,
            env=env,
        )

    def common(self):
        return [
            "--repository",
            REPOSITORY,
            "--tag",
            TAG,
            "--commit",
            COMMIT,
            "--archive",
            str(self.archive),
        ]

    def test_build_and_verify_modes(self):
        source = make_tree(self.root / "src")
        stage = str(self.root / "stage")
        result = self.run_script(
            *self.common(), "--source", str(source), "--output", stage
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((self.root / "stage" / bundle.MANIFEST_NAME).is_file())
        self.assertEqual((self.root / "stage/index.html").stat().st_mtime, 1)
        result = self.run_script(
            *self.common(), "--source", str(source), "--output", stage
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn("already exists", result.stderr)
        result = self.run_script(
            *self.common(), "--verify-deb", str(self.root / "no.deb")
        )
        self.assertEqual(result.returncode, 1)

    def test_epoch_is_required_in_build_mode(self):
        source = make_tree(self.root / "src")
        result = self.run_script(
            *self.common(),
            "--source",
            str(source),
            "--output",
            str(self.root / "stage"),
            epoch=None,
        )
        self.assertEqual(result.returncode, 2)

    def test_argument_errors(self):
        self.assertEqual(self.run_script(*self.common()).returncode, 2)
        self.assertEqual(
            self.run_script(
                *self.common(), "--verify-deb", "x", "--source", "y"
            ).returncode,
            2,
        )


GOLDEN_DEB = FIXTURES / f"proto-os_{TAG}_all.deb"
GOLDEN_STATEMENT = FIXTURES / f"proto-os_{TAG}_all.deb.statement.json"
GOLDEN_REPOSITORY = "btc-mining/proto-fleet-internal"
GOLDEN_ISSUED_AT = "2026-11-18T12:00:00Z"
SIGN_SCRIPT = TESTS.parent / "scripts/sign-protoos-release.sh"


class ReleaseStatementTests(TemporaryDirectoryCase):
    def test_matches_frigate_bytes(self):
        statement = bundle.release_statement(
            GOLDEN_DEB, GOLDEN_REPOSITORY, TAG, GOLDEN_ISSUED_AT
        )
        self.assertEqual(statement, GOLDEN_STATEMENT.read_bytes())

    def test_fields(self):
        statement = json.loads(
            bundle.release_statement(GOLDEN_DEB, REPOSITORY, TAG, GOLDEN_ISSUED_AT)
        )
        data = GOLDEN_DEB.read_bytes()
        self.assertEqual(
            statement,
            {
                "schema": "frigate-release-statement/v1",
                "package": "proto-os",
                "version": expected_debian_version(TAG),
                "architecture": "all",
                "repository": REPOSITORY,
                "sha256": hashlib.sha256(data).hexdigest(),
                "size": len(data),
                "min_installed_version": "",
                "issued_at": GOLDEN_ISSUED_AT,
            },
        )
        self.assertEqual(
            list(statement),
            [
                "schema",
                "package",
                "version",
                "architecture",
                "repository",
                "sha256",
                "size",
                "min_installed_version",
                "issued_at",
            ],
        )

    def test_version_follows_the_tag(self):
        for tag in TAG_CASES:
            with self.subTest(tag=tag):
                statement = json.loads(
                    bundle.release_statement(
                        GOLDEN_DEB, REPOSITORY, tag, GOLDEN_ISSUED_AT
                    )
                )
                self.assertEqual(statement["version"], expected_debian_version(tag))

    def test_rejects_bad_issued_at(self):
        for issued_at in (
            "",
            "2026-11-18T12:00:00",
            "2026-11-18T12:00:00.5Z",
            "2026-11-18T12:00:00+00:00",
            "2026-11-18 12:00:00Z",
            "2026-11-18T2:00:00Z",
            "2026-02-30T12:00:00Z",
            "2026-11-18T12:00:00Z\n",
            None,
        ):
            with self.subTest(issued_at=issued_at):
                with self.assertRaises(bundle.BundleError):
                    bundle.release_statement(GOLDEN_DEB, REPOSITORY, TAG, issued_at)

    def test_rejects_bad_inputs(self):
        empty = self.root / "empty.deb"
        empty.write_bytes(b"")
        link = self.root / "link.deb"
        link.symlink_to(GOLDEN_DEB)
        cases = (
            (GOLDEN_DEB, "no-slash", TAG),
            (GOLDEN_DEB, "owner/name/extra", TAG),
            (GOLDEN_DEB, "-owner/name", TAG),
            (GOLDEN_DEB, REPOSITORY, "0.6.2"),
            (empty, REPOSITORY, TAG),
            (link, REPOSITORY, TAG),
            (self.root, REPOSITORY, TAG),
            (self.root / "missing.deb", REPOSITORY, TAG),
        )
        for deb, repository, tag in cases:
            with self.subTest(deb=deb.name, repository=repository, tag=tag):
                with self.assertRaises(bundle.BundleError):
                    bundle.release_statement(deb, repository, tag, GOLDEN_ISSUED_AT)

    def test_rejects_oversized_package(self):
        limits = bundle.PackageLimits(
            **{**bundle.DEFAULT_PACKAGE_LIMITS._asdict(), "max_package_bytes": 10}
        )
        with self.assertRaises(bundle.BundleError):
            bundle.release_statement(
                GOLDEN_DEB, REPOSITORY, TAG, GOLDEN_ISSUED_AT, package_limits=limits
            )

    def test_write_statement_never_replaces(self):
        path = self.root / "statement.json"
        bundle.write_statement(path, b"first")
        with self.assertRaises(FileExistsError):
            bundle.write_statement(path, b"second")
        self.assertEqual(path.read_bytes(), b"first")


class StatementCommandLineTests(DebBuilder, TemporaryDirectoryCase):
    run_script = CommandLineTests.run_script
    common = CommandLineTests.common

    def test_verify_and_write_statement(self):
        deb = self.build()
        statement_path = self.root / "statement.json"
        result = self.run_script(
            *self.common(),
            "--verify-deb",
            str(deb),
            "--write-statement",
            str(statement_path),
            "--issued-at",
            GOLDEN_ISSUED_AT,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            statement_path.read_bytes(),
            bundle.release_statement(deb, REPOSITORY, TAG, GOLDEN_ISSUED_AT),
        )
        result = self.run_script(
            *self.common(),
            "--verify-deb",
            str(deb),
            "--write-statement",
            str(statement_path),
        )
        self.assertEqual(result.returncode, 1)

    def test_default_issued_at_is_now(self):
        deb = self.build()
        statement_path = self.root / "statement.json"
        result = self.run_script(
            *self.common(),
            "--verify-deb",
            str(deb),
            "--write-statement",
            str(statement_path),
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        issued_at = json.loads(statement_path.read_bytes())["issued_at"]
        self.assertEqual(bundle.parse_issued_at(issued_at), issued_at)

    def test_rejected_package_writes_no_statement(self):
        deb = self.build(control=control_text(Package="other"))
        statement_path = self.root / "statement.json"
        result = self.run_script(
            *self.common(),
            "--verify-deb",
            str(deb),
            "--write-statement",
            str(statement_path),
        )
        self.assertEqual(result.returncode, 1)
        self.assertFalse(statement_path.exists())

    def test_argument_errors(self):
        source = make_tree(self.root / "src2")
        for args in (
            (
                "--source",
                str(source),
                "--output",
                str(self.root / "out"),
                "--write-statement",
                str(self.root / "s.json"),
            ),
            ("--verify-deb", "x", "--issued-at", GOLDEN_ISSUED_AT),
        ):
            with self.subTest(args=args):
                self.assertEqual(self.run_script(*self.common(), *args).returncode, 2)


FAKE_AWS = r"""#!/bin/sh
# Fake AWS CLI: answers kms get-public-key and kms sign from local key files.
set -eu
[ "$1" = kms ] || exit 64
case "$2" in
  get-public-key)
    openssl pkey -in "$FAKE_AWS_PUBLIC_KEY_PRIVATE" -pubout -outform DER | base64
    ;;
  sign)
    message=""
    for arg in "$@"; do
      case "$arg" in fileb://*) message="${arg#fileb://}" ;; esac
    done
    if [ -n "${FAKE_AWS_SIGNATURE:-}" ]; then
      base64 < "$FAKE_AWS_SIGNATURE"
    else
      openssl dgst -sha256 -sign "$FAKE_AWS_SIGNING_KEY" "$message" | base64
    fi
    ;;
  *) exit 64 ;;
esac
"""


@unittest.skipUnless(
    shutil.which("openssl") and shutil.which("bash") and shutil.which("sha256sum"),
    "needs openssl, bash and sha256sum",
)
class SignScriptTests(DebBuilder, TemporaryDirectoryCase):
    def setUp(self):
        super().setUp()
        self.assets = self.root / "assets"
        self.assets.mkdir()
        self.deb = self.assets / f"proto-os_{TAG}_all.deb"
        self.build().rename(self.deb)
        shutil.copy(self.archive, self.assets / self.archive.name)
        self.write_sidecar()
        self.bin = self.root / "bin"
        self.bin.mkdir()
        aws = self.bin / "aws"
        aws.write_text(FAKE_AWS)
        aws.chmod(0o755)
        self.key = self.make_key("kms", "prime256v1")

    def make_key(self, name, curve):
        path = self.root / f"{name}.pem"
        subprocess.run(
            ["openssl", "ecparam", "-name", curve, "-genkey", "-noout", "-out", path],
            check=True,
            capture_output=True,
        )
        return path

    def key_id(self, key):
        der = subprocess.run(
            ["openssl", "pkey", "-in", key, "-pubout", "-outform", "DER"],
            check=True,
            capture_output=True,
        ).stdout
        return "sha256:" + hashlib.sha256(der).hexdigest()

    def write_sidecar(self, digest=None):
        digest = digest or hashlib.sha256(self.deb.read_bytes()).hexdigest()
        (self.assets / f"{self.deb.name}.sha256").write_text(
            f"{digest}  {self.deb.name}\n"
        )

    def sign(self, *extra, public=None, signer=None, signature=None):
        env = dict(os.environ)
        env["PATH"] = f"{self.bin}{os.pathsep}{env['PATH']}"
        env["FAKE_AWS_PUBLIC_KEY_PRIVATE"] = str(public or self.key)
        env["FAKE_AWS_SIGNING_KEY"] = str(signer or self.key)
        env.pop("FAKE_AWS_SIGNATURE", None)
        if signature is not None:
            env["FAKE_AWS_SIGNATURE"] = str(signature)
        return subprocess.run(
            [
                "bash",
                str(SIGN_SCRIPT),
                "--dir",
                str(self.assets),
                "--tag",
                TAG,
                "--repository",
                REPOSITORY,
                "--commit",
                COMMIT,
                "--kms-key-id",
                "alias/protoos-release",
                *extra,
            ],
            capture_output=True,
            text=True,
            check=False,
            env=env,
        )

    def outputs(self):
        statement = Path(f"{self.deb}.statement.json")
        return statement, Path(f"{self.deb}.statement.sig")

    def assert_refused(self, result, message):
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn(message, result.stderr)
        for path in self.outputs():
            self.assertFalse(path.exists(), path)

    def test_signs_and_verifies(self):
        result = self.sign(
            "--issued-at",
            GOLDEN_ISSUED_AT,
            "--expected-key-id",
            self.key_id(self.key),
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), self.key_id(self.key))
        statement, signature = self.outputs()
        self.assertEqual(
            statement.read_bytes(),
            bundle.release_statement(self.deb, REPOSITORY, TAG, GOLDEN_ISSUED_AT),
        )
        public = self.root / "public.pem"
        subprocess.run(
            ["openssl", "pkey", "-in", self.key, "-pubout", "-out", public],
            check=True,
            capture_output=True,
        )
        verified = subprocess.run(
            [
                "openssl",
                "dgst",
                "-sha256",
                "-verify",
                public,
                "-signature",
                signature,
                statement,
            ],
            capture_output=True,
            check=False,
        )
        self.assertEqual(verified.returncode, 0, verified.stderr)
        # A second run never replaces the signed files.
        self.assert_refused_keeps(self.sign(), "already exists")

    def assert_refused_keeps(self, result, message):
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(message, result.stderr)
        for path in self.outputs():
            self.assertTrue(path.exists(), path)

    def test_refuses_unbound_sidecar(self):
        self.write_sidecar("0" * 64)
        self.assert_refused(self.sign(), "is not bound")

    def test_refuses_package_that_fails_the_contract(self):
        rejected = self.build(control=control_text(Package="other"))
        self.deb.write_bytes(rejected.read_bytes())
        self.write_sidecar()
        self.assert_refused(self.sign(), "")

    def test_refuses_unpinned_key(self):
        other = self.make_key("other", "prime256v1")
        self.assert_refused(
            self.sign("--expected-key-id", self.key_id(other)), "is not the expected"
        )

    def test_refuses_malformed_pin(self):
        self.assert_refused(
            self.sign("--expected-key-id", "sha256:XYZ"), "--expected-key-id"
        )

    def test_refuses_other_curve(self):
        key = self.make_key("p384", "secp384r1")
        self.assert_refused(
            self.sign(public=key, signer=key), "is not an ECDSA P-256 key"
        )

    def test_refuses_signature_from_another_key(self):
        other = self.make_key("other", "prime256v1")
        self.assert_refused(self.sign(signer=other), "does not verify")

    def test_refuses_oversized_signature(self):
        blob = self.root / "big.sig"
        blob.write_bytes(b"\x30" * 129)
        self.assert_refused(self.sign(signature=blob), "Frigate accepts 1 to 128")

    def test_refuses_empty_signature(self):
        blob = self.root / "empty.sig"
        blob.write_bytes(b"")
        self.assert_refused(self.sign(signature=blob), "Frigate accepts 1 to 128")

    def test_refuses_missing_assets(self):
        (self.assets / self.archive.name).unlink()
        self.assert_refused(self.sign(), "is missing")

    def test_usage(self):
        result = subprocess.run(
            ["bash", str(SIGN_SCRIPT), "--dir"],
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(result.returncode, 2)
        self.assertIn("Usage", result.stderr)


if __name__ == "__main__":
    unittest.main()
