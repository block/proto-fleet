manage-git = false
env = {
  "PATH": "${HERMIT_ENV}/bin/scripts:${PATH}",
}
sources = [
  "https://github.com/cashapp/hermit-packages.git",
  "env:///hermit-packages",
]

github-token-auth {
}
