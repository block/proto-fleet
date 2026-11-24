// cashapp/hermit-packages does not list Go 1.26.11 yet. Delete this file and
// restore the source order in bin/hermit.hcl once it does.
description = "Go is an open source programming language that makes it easy to build simple, reliable, and efficient software."
binaries = ["bin/*"]
env = {
  "GOROOT": "${root}",
  "GOBIN": "${HERMIT_ENV}/.hermit/go/bin",
  "PATH": "${GOBIN}:${PATH}",
  "GOTOOLCHAIN": "local",
}
strip = 1
test = "go version"
repository = "https://github.com/golang/go"
source = "https://golang.org/dl/go${version}.${os}-${arch}.tar.gz"

version "1.26.11" {}

sha256sums = {
  "https://golang.org/dl/go1.26.11.darwin-amd64.tar.gz": "394baae971332f2bc783d6e2004ae57101479bd3b06ed8209a3fe3570d4ce498",
  "https://golang.org/dl/go1.26.11.darwin-arm64.tar.gz": "e3a42e67a7a8acf450ebada2b074c7ca863000ca514907b7185b45e8f5cdff57",
  "https://golang.org/dl/go1.26.11.linux-amd64.tar.gz": "9849f4e04ee35303714e34fbc3ac8ab44ea3e448a95aa182f91e3b9b79c710ec",
  "https://golang.org/dl/go1.26.11.linux-arm64.tar.gz": "6c2f29b458b5add0835f18b776f69c8881a6d88428f1484a6da55755e620757e",
}
