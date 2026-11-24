# Integration Testing Guide

The Proto plugin tests run against the simulators in this repository. They do
not use firmware builds or containers from other repositories.

| Simulator | Simulates | How the tests run it |
| --- | --- | --- |
| [`server/fake-proto-rig`](../../../server/fake-proto-rig/README.md) | Proto rig, MDK v1 REST API | Docker image built by testcontainers, or a local binary |
| [`server/fake-ct1`](../../../server/fake-ct1/README.md) | CT1 container, MDK v2 gRPC API | Local binary |

## Tests

| Tests | Location | Requirements |
| --- | --- | --- |
| Unit tests | `tests/unit/`, and `*_test.go` files in `internal/` and `pkg/` | None |
| Rig lifecycle: `TestProtoPluginIntegration` | `tests/integration_test.go` | Docker |
| Rig discovery: `TestDiscoverDevice_*` | `internal/driver/discovery_test.go` | Docker |
| Rig load fixture: `TestLoadFixtureProtoRESTContract` | `internal/driver/load_fixture_test.go` | Go toolchain, a second local IPv4 address |
| CT1 driver: `TestCT1Simulator*` | `internal/driver/ct1_simulator_test.go` | Go toolchain |
| CT1 firmware, PSU firmware, and controller software updates: `Test*Simulator*` | `internal/ct1/` | Go toolchain; `TestModuleMiningSimulatorLeavesNeighborsUnaffected` also needs `CT1_SIMULATOR_BIN` |
| HA lab: `TestFrigateHALab` | `internal/ct1/ha_lab_test.go` | `CT1_HA_LAB_STATE` set to a running Frigate HA lab |

The Docker tests fail when Docker is not available. `go test -short ./...`
skips them and the load fixture test. A test that needs a variable or a second
IPv4 address skips itself when that is missing.

The Docker tests build the image from `server/fake-proto-rig/Dockerfile` and
start containers from it. The CT1 tests build `server/fake-ct1` one time for
each test package. Set `CT1_SIMULATOR_BIN` to a built `fake-ct1` binary to skip
that build:

```bash
(cd ../../server/fake-ct1 && GOWORK=off go build -o /tmp/fake-ct1 .)
export CT1_SIMULATOR_BIN=/tmp/fake-ct1
```

## Commands

Run these from `plugin/proto`:

```bash
just test-unit          # tests/unit only
just test-integration   # tests/, with a 30-minute timeout
just test               # all tests
just test-long          # all tests, with a 30-minute timeout
just check              # lint and test-long, as CI runs it

go test -short ./...    # skip the Docker tests
go test -run TestProtoPluginIntegration -v ./tests/...
```

## Continuous integration

The `Proto Plugin Checks` workflow
(`.github/workflows/protofleet-proto-plugin-checks.yml`) waits for the Docker
daemon, pulls the base images of the `fake-proto-rig` Dockerfile, builds
`fake-ct1`, sets `CT1_SIMULATOR_BIN`, and runs `just check`.

## Troubleshooting

- **Cannot connect to the Docker daemon.** Start Docker, then run the tests
  again.
- **The image build fails to pull a base image.** Pull the base images of
  `server/fake-proto-rig/Dockerfile` first, as the CI workflow does.
- **A test skips.** Read the skip message. It names the missing binary,
  variable, or address.
- **A simulator response does not match the plugin.** The simulators are
  maintained by hand against the vendored contracts in
  [`proto-rig-api`](../../../proto-rig-api/README.md) and
  [`sdk/mdk`](../../../sdk/mdk/README.md). Update the simulator and the plugin
  together.
