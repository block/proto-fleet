#!/usr/bin/env python3
"""protoc-gen-python-grpc — A proper protoc plugin for Python gRPC stub generation.

Reads a CodeGeneratorRequest from stdin, generates *_pb2_grpc.py files using
grpc_tools.protoc with --descriptor_set_in, optionally rewrites imports and
injects __init__.py files, then writes a CodeGeneratorResponse to stdout.

Custom options (passed via buf.gen.yaml `opt`):
  import_prefix=<pkg>   Rewrite imports in _pb2_grpc.py to use package-qualified paths
  init_files=true|false  Generate __init__.py in every output directory (default: true)
"""

import os
import re
import sys
import tempfile

from google.protobuf.compiler.plugin_pb2 import (
    CodeGeneratorRequest,
    CodeGeneratorResponse,
)
from google.protobuf.descriptor_pb2 import FileDescriptorSet


def parse_options(parameter):
    """Parse comma-separated key=value options from the protoc parameter string."""
    opts = {}

    if not parameter:
        return opts

    for opt in parameter.split(","):
        if "=" in opt:
            key, value = opt.split("=", 1)
            opts[key] = value
        else:
            opts[opt] = "true"

    return opts


def rewrite_imports(content, prefix):
    """Rewrite pb2 imports in _pb2_grpc.py content to use a package prefix.

    Handles two protoc import styles, skipping well-known types (google.*):
      Packaged: "from pkg.v1 import foo_pb2 as ..." → "from <prefix>.pkg.v1 import foo_pb2 as ..."
      Flat:     "import foo_pb2 as ..."             → "from <prefix> import foo_pb2 as ..."
    """
    content = re.sub(
        r"^(from )((?!google\.)\S+)( import \S+_pb2 as .*)$",
        rf"\g<1>{prefix}.\2\3",
        content,
        flags=re.MULTILINE,
    )
    content = re.sub(
        r"^import (\S+_pb2) as (.*)$",
        rf"from {prefix} import \1 as \2",
        content,
        flags=re.MULTILINE,
    )
    return content


def collect_init_dirs(file_names):
    """Collect all directory paths that need __init__.py files."""
    dirs = set()
    for name in file_names:
        parts = name.split("/")
        for i in range(1, len(parts)):
            dirs.add("/".join(parts[:i]))
    return sorted(dirs)


def main():
    if "--help" in sys.argv or "-h" in sys.argv:
        print(__doc__.strip())
        sys.exit(0)

    request = CodeGeneratorRequest()
    request.ParseFromString(sys.stdin.buffer.read())

    opts = parse_options(request.parameter)
    import_prefix = opts.get("import_prefix", "")
    init_files = opts.get("init_files", "true").lower() != "false"

    fds = FileDescriptorSet()
    fds.file.extend(request.proto_file)

    with tempfile.TemporaryDirectory() as tmpdir:
        fds_path = os.path.join(tmpdir, "descriptors.bin")
        with open(fds_path, "wb") as f:
            f.write(fds.SerializeToString())

        output_dir = os.path.join(tmpdir, "output")
        os.makedirs(output_dir)

        from grpc_tools import protoc

        protoc_args = [
            "protoc",
            f"--descriptor_set_in={fds_path}",
            f"--grpc_python_out={output_dir}",
        ]
        protoc_args.extend(request.file_to_generate)

        exit_code = protoc.main(protoc_args)
        if exit_code != 0:
            response = CodeGeneratorResponse()
            response.error = f"grpc_tools.protoc failed with exit code {exit_code}"
            sys.stdout.buffer.write(response.SerializeToString())
            return

        response = CodeGeneratorResponse()
        response.supported_features = CodeGeneratorResponse.FEATURE_PROTO3_OPTIONAL

        generated_names = []
        for root, _, files in os.walk(output_dir):
            for filename in files:
                filepath = os.path.join(root, filename)
                relpath = os.path.relpath(filepath, output_dir)
                with open(filepath, "r") as f:
                    content = f.read()

                if import_prefix and filename.endswith("_pb2_grpc.py"):
                    content = rewrite_imports(content, import_prefix)

                out_file = response.file.add()
                out_file.name = relpath
                out_file.content = content
                generated_names.append(relpath)

        if init_files:
            existing = {f.name for f in response.file}
            for dir_path in collect_init_dirs(generated_names):
                init_path = f"{dir_path}/__init__.py"
                if init_path not in existing:
                    init_file = response.file.add()
                    init_file.name = init_path
                    init_file.content = ""

    sys.stdout.buffer.write(response.SerializeToString())


if __name__ == "__main__":
    main()
