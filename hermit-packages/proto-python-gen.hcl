description = "Python gRPC code generation with Buf"
binaries = ["buf-gen-python"]
test = "buf-gen-python --help"

on "unpack" {
  run {
    cmd = "bash"
    args = ["${root}/setup.sh"]
  }
  run {
    cmd = "chmod"
    args = ["+x", "${root}/bin/buf-gen-python"]
  }
}

version "0.1.0" {
  source = "file://${env.HERMIT_ENV}/packages/proto-python-gen/proto-python-gen-${version}.tar.gz"
}
