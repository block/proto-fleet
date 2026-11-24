manage-git = false
env = {
  "PATH": "${HERMIT_ENV}/bin/scripts:${PATH}",
}
// The local source comes first so hermit-packages/go.hcl takes precedence.
sources = [
  "env:///hermit-packages",
  "https://github.com/cashapp/hermit-packages.git",
]

github-token-auth {
}
