# Raspberry Pi Deployment Workflows

This document describes the GitHub workflows for deploying ProtoFleet to Raspberry Pi devices.

## Overview

ProtoFleet can be deployed to Raspberry Pi devices in two ways:

1. **Manual Deployment** (`protofleet-deploy-to-pi.yml`): Deploy from any branch to a specific Pi via workflow dispatch
2. **Automatic Release Deployment** (`release.yml`): Automatically deploy to all Pis when a new release is published

Both workflows use self-hosted GitHub Actions runners on the Raspberry Pis and share a common deployment action for consistency.

## Workflow: `protofleet-deploy-to-pi.yml` (Manual)

This workflow allows manual deployment of ProtoFleet from any branch to a specified Raspberry Pi.

### Triggering the Workflow

The workflow uses `workflow_dispatch` for manual triggering through the GitHub Actions UI.

**Required Inputs:**

- **branch**: The git branch to build from (default: `main`)
- **environment**: The deployment environment (Raspberry Pi location) to deploy to. Choose from:
  - `pi-stl` - St. Louis location
  - `pi-mar` - Marina location
  - `pi-fxsj` - FXSJ location

### Workflow Steps

The workflow consists of four jobs that run sequentially:

#### 1. `build-proto-fleet-server`

- Checks out the specified branch
- Sets up Go build environment with Hermit
- Builds server binaries for both amd64 and arm64 architectures
- Builds plugin binaries (proto-plugin and antminer-plugin) for both architectures
- Creates a version.txt file with build metadata
- Packages everything into a tarball
- Uploads as a GitHub Actions artifact

#### 2. `build-proto-fleet-client`

- Checks out the specified branch
- Sets up Node.js build environment
- Installs npm dependencies
- Builds the ProtoFleet client application
- Creates a version.txt file with build metadata
- Packages the client into a tarball
- Uploads as a GitHub Actions artifact

#### 3. `build-deployment-bundle`

- Downloads both server and client artifacts from previous jobs
- Creates the deployment directory structure
- Extracts server and client artifacts into the deployment structure
- Copies all deployment configuration files:
  - Docker Compose configuration
  - Dockerfiles for client and server
  - run-fleet.sh deployment script
  - TimescaleDB configuration
- Creates a comprehensive version.txt file
- Packages everything into a single deployment tarball
- Uploads the deployment bundle as an artifact

#### 4. `deploy-to-pi`

- Uses the shared composite action `.github/actions/deploy-protofleet` for consistent deployment logic
- Downloads the deployment bundle artifact
- Deploys ProtoFleet to the target Pi:
  - Determines installation directory (from input or default)
  - Backs up existing `.env` file if present (preserves database credentials)
  - Extracts the deployment bundle
  - Restores the `.env` file
  - Runs `run-fleet.sh` which:
    - Checks for and installs Docker if needed
    - Validates/generates environment variables (DB passwords, encryption keys)
    - Pulls Docker images
    - Builds Docker containers for the correct architecture (arm64/amd64)
    - Starts all services via Docker Compose
- Displays the deployed version information

### Reusable Deployment Action

A shared composite action (`.github/actions/deploy-protofleet`) encapsulates the deployment logic used by both the manual deployment workflow and the automatic release deployment. This ensures:

- **Consistency**: Same deployment process across manual and automated deployments
- **Maintainability**: Single source of truth for deployment logic
- **Reusability**: Easy to add new deployment targets without code duplication

**Inputs:**

- `artifact_name`: Name of the deployment bundle artifact to download
- `install_dir`: Optional installation directory (defaults to `$HOME/proto-fleet`)

**Steps:**

1. Downloads the specified deployment bundle artifact
2. Extracts and deploys to the target directory
3. Preserves existing `.env` files across updates
4. Runs the deployment script (`run-fleet.sh`)
5. Verifies deployment by checking container status

Both `protofleet-deploy-to-pi.yml` (manual) and `release.yml` (automatic) use this shared action.

### Deployment Process Details

The deployment follows the same pattern as the install.sh script:

1. **Environment Preservation**: Existing `.env` files are backed up and restored to prevent loss of database credentials during upgrades

2. **Docker Setup**: The `run-fleet.sh` script ensures Docker and Docker Compose are installed and running

3. **Architecture Detection**: Automatically detects ARM64 vs AMD64 architecture and uses the appropriate binaries

4. **Service Management**:
   - Stops existing containers
   - Builds new images with the latest code
   - Starts all services (TimescaleDB, Fleet API, Frontend)

5. **Plugin Validation**: Ensures all required plugin binaries are present and executable

### Usage Example

To deploy the latest code from the `main` branch to a Raspberry Pi:

1. Navigate to **Actions** → **ProtoFleet Deploy to Raspberry Pi**
2. Click **Run workflow**
3. Fill in the inputs:
   - **branch**: `main`
   - **environment**: Select the target location (e.g., `pi-stl`, `pi-mar`, or `pi-fxsj`)
4. Click **Run workflow**

### Adding New Deployment Locations

To add a new Raspberry Pi deployment location:

1. **Set up the Pi as a self-hosted runner**:
   - Navigate to Settings → Actions → Runners → [New self-hosted runner](https://github.com/proto-at-block/proto-fleet/settings/actions/runners/new?arch=arm64&os=linux)
   - Follow the instructions to install ARM64 Architecture and configure the runner on the Pi
   - In addition to the default labels, add the following labels to the runner (comma separated):
     - `proto-fleet-rpi`
     - `pi-new-location` (the environment name)
   - Configure the self-hosted runner as a [service](https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/configure-the-application) on the pi

2. **Update both workflow files** to include the new location:

   In `protofleet-deploy-to-pi.yml`:

   ```yaml
   environment:
     description: 'Deployment environment (Raspberry Pi location)'
     required: true
     type: choice
     options:
       - pi-stl
       - pi-mar
       - pi-fxsj
       - pi-dalton
       - pi-new-location  # Add your new location here
   ```

   **For staged release deployments**, decide if the new location should be:

   - **Testing environment** (auto-deploys first):
     Update `release.yml` (deploy-to-testing-env job):

     ```yaml
     runs-on: [self-hosted, proto-fleet-rpi, 'pi-new-location']
     ```

   - **Production environment** (requires approval):
     Update `release.yml` (deploy-to-all-envs job):

     ```yaml
     strategy:
       matrix:
         environment: [pi-stl, pi-fxsj, pi-dalton, pi-new-location]
     ```

The new location will now be:

- Available for manual deployments via the workflow dispatch UI
- Included in either the testing or production stage of release deployments

### Deploying to Multiple Locations (Release Workflow)

**Staged deployment to Raspberry Pis is implemented in the `release.yml` workflow!**

When a new release is published (non-prerelease), the workflow uses a two-stage deployment approach:

#### Stage 1: Testing Environment (`deploy-to-testing-env`)

- Automatically deploys to **pi-mar** (testing environment)
- No manual approval required
- Allows validation of the release before production deployment
- Uses the `testing-env` GitHub environment (no protection rules)
- Runs immediately after build artifacts are created
- **Timeout**: 30 minutes - job fails gracefully if runner is offline
- **Health check**: Verifies runner is online and has sufficient disk space

#### Stage 2: Production Environments (`deploy-to-all-envs`)

- Deploys to **pi-stl**, **pi-fxsj**, and **pi-dalton** in parallel
- **Requires manual approval** before deployment begins
- Only starts after testing environment deployment succeeds
- Uses the `all-envs` GitHub environment (configured with required reviewers)
- Independent failures via `fail-fast: false` - one Pi failure doesn't stop others
- **Timeout**: 30 minutes per Pi - job fails gracefully if runner is offline
- **Health check**: Each Pi verifies runner is online before deployment

**Deployment Flow:**

```
1. Build artifacts (client, server, full deployment bundle)
   ↓
2. Deploy to pi-mar (testing-env) - AUTOMATIC
   ↓
   [Validate deployment on pi-mar]
   ↓
3. Workflow pauses and waits for manual approval
   ↓
   [Reviewer approves in GitHub UI]
   ↓
4. Deploy to pi-stl, pi-fxsj, pi-dalton (all-envs) - IN PARALLEL
```

**Key Features:**

- Safe deployment with testing validation before production
- Manual approval gate prevents accidental production deployments
- Clear audit trail of who approved production deployments
- Parallel deployment to production Pis for faster rollout
- Reuses artifacts from the build phase (efficient, no rebuild)

### Approving Production Deployments

When the workflow reaches the `deploy-to-all-envs` job:

1. **GitHub sends notifications** to required reviewers
2. **Navigate to the workflow run** in the Actions tab
3. You'll see a yellow banner: **"Review required for all-envs"**
4. Click **Review deployments**
5. Select the **all-envs** environment checkbox
6. (Optional) Add a comment about validation performed on pi-mar
7. Click **Approve and deploy**

The deployment to the 3 production Pis will begin immediately after approval.

### Additional Resources

- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [Raspberry Pi SSH Setup](https://www.raspberrypi.com/documentation/computers/remote-access.html#ssh)
