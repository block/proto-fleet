---
title: "Pre-baked ProtoFleet E2E runner"
date: 2026-08-31
status: implementing
type: plan
---

# Pre-baked ProtoFleet E2E runner

## Context

The August 2026 CI timing baseline found that successful ProtoFleet E2E jobs
spend 47.5% of their runner time outside tests. Downloading and loading the
Docker Compose image bundle accounts for 1,013 runner-minutes in the sampled
runs, or about 66 seconds per shard. Repeating npm and Playwright setup adds
more overhead across the 32 functional shards and two visual jobs.

A GitHub Actions job container cannot remove the largest cost: the Compose
stack runs through the runner host's Docker daemon, whose image store is not
populated by layers in the job container. The supported pre-warmed topology is
therefore a GitHub-hosted larger runner using a custom VM image.

## Repository implementation

The `ProtoFleet E2E Runner Image` workflow generates the `protofleet-e2e`
custom image from the default branch. It installs:

- the exact `client/package-lock.json` dependency tree;
- the matching Playwright Chromium build and operating-system dependencies;
- all pulled and locally built images reported by `server/docker-compose.yaml`.

The image records the GitHub `hashFiles` digests for `server/**` and the client
lockfile. E2E jobs use a component only when its digest matches. They also
verify that every recorded Docker image still exists in the host daemon. A
miss uses the existing Actions cache, artifact, and install path.

Scheduled E2E runs deliberately ignore pre-baked Docker images so the existing
daily rebuild against fresh upstream bases remains intact. The build job still
publishes a Docker image artifact on a hit, allowing a shard on an older runner
image to fall back safely during image rollouts.

## Organization setup

An organization or CI/CD administrator must complete these steps after the
repository changes land:

1. Enable GitHub-hosted custom images for the organization.
2. Create a dedicated Linux x64 image-generation larger runner from a
   GitHub-owned Ubuntu image. Give only this repository access to its runner
   group and enable custom-image generation.
3. Set the repository or organization variable
   `PROTOFLEET_E2E_IMAGE_BUILDER_RUNNER` to that runner's name.
4. Run `ProtoFleet E2E Runner Image` from the default branch. Its successful
   `snapshot` job creates image `protofleet-e2e` version `1.x`.
5. Create a Linux x64 larger runner using the latest `protofleet-e2e` image.
   Its maximum concurrency must accommodate the build, 32 functional shards,
   and two visual jobs without serializing the existing matrix.
6. Give this repository access and set `PROTOFLEET_E2E_RUNNER` to the runtime
   runner's name. Until this variable exists, all jobs continue using
   `ubuntu-latest`.

Use a dedicated image-generation runner group. The image workflow refuses to
run from a non-default branch, and the runtime runner contains no credentials.

## Verification and rollout

1. Leave `PROTOFLEET_E2E_RUNNER` unset while the image is generated.
2. Confirm the image manifest summary lists every Compose image and that a
   manually configured runtime runner reports Docker, Playwright, and client
   dependency hits.
3. Set the runtime variable and exercise the same commit with the stock and
   custom runner configurations. Compare at least ten full E2E runs with:

   ```bash
   GITHUB_TOKEN=... .github/scripts/analyze_ci_timings.py \
     --baseline .github/scripts/ci_timing_baseline.json \
     --json-out /tmp/protofleet-e2e-runner-after.json \
     --markdown-out /tmp/protofleet-e2e-runner-after.md
   ```

4. Check failure and cancellation rates in addition to timing. The primary
   target is to reduce mean ProtoFleet shard overhead from the 1.94-minute
   baseline without increasing E2E failures.

Rollback is immediate: delete or clear `PROTOFLEET_E2E_RUNNER`. The workflow
then resolves `runs-on` to `ubuntu-latest`, and every cache probe becomes a
normal miss.

## Remaining external dependency

Repository code cannot create or grant access to organization-level larger
runners. The custom image remains inactive until an administrator provisions
the image-generation and runtime runners and sets the two variables above.
