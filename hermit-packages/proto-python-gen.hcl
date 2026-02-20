description = "Python gRPC protoc plugin for use with Buf"
binaries = ["bin/protoc-gen-python-grpc"]
test = "protoc-gen-python-grpc --help"

on "unpack" {
  run {
    cmd = "/bin/bash"
    args = ["${root}/setup.sh"]
  }
  run {
    cmd = "/bin/chmod"
    args = ["+x", "${root}/bin/protoc-gen-python-grpc"]
  }
}

version "0.2.0" {
  source = "file://${HERMIT_ENV}/packages/proto-python-gen/proto-python-gen-${version}.tar.gz"
}
