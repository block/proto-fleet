# Proto Development Deployment Documentation

## Infrastructure

We use AWS to host all of our services and GitHub Actions to deploy them. 

Our infrastructure consists of the following key components:

- 4 EC2 instances:
  - 3 dedicated miner instances
  - 1 instance for remaining services (databases, API servers, etc.)
- ECR (Elastic Container Registry)
- Route 53 for DNS management
- IAM roles and permissions

### EC2 Instances

- **Miner Instances (x3)**
  - Used for running miner simulators for testing ProtoFleet

- **Services Instance (x1)**
  - Hosts all other services like backends, frontends, etc.

#### Access Control

All EC2 instances are accessible only using one of the following methods:

- Cloudflare WARP VPN
- AWS EC2 Instance Connect Endpoint
- AWS SSM

#### Firewall configuration

- **Web Server Security Group:**
  - Used by all EC2 instances
  - Inbound Rules:
    - Port 22 (SSH): Access from Instance Connect endpoint (used by GitHub Actions) and via Cloudflare WARP VPN
    - Port 80 (HTTP): Access via Cloudflare WARP VPN
    - Port 443 (HTTPS): Access via Cloudflare WARP VPN
    - Port 2121 (gRPC API of Miners): Access via Cloudflare WARP VPN
  - Outbound Rules:
    - All traffic allowed (0.0.0.0/0)

### Container Registry (ECR)

We use Amazon Elastic Container Registry (ECR) for storing our Docker images deployed to EC2 instances.

#### Repositories

We have the following ECR repositories:

- `protofleet/backend-develop` - Backend images
- `protofleet/frontend-dashboard-develop` - Frontend dashboard images
- `protofleet/frontend-storybook-develop` - Frontend storybook images
- `protofleet/miner-sim-develop` - Miner simulator images

#### Image Tagging Conventions

We use the following tags:

1. **Main Branch Deployments**
   - Tag: `main`
   - Example: `protofleet/backend-develop:main`
   - Applies to: `protofleet/backend-develop`, `protofleet/frontend-dashboard-develop`, `protofleet/frontend-storybook-develop`

2. **Pull Request Deployments**
   - Tag format: `pr-{number}`
   - Example: `protofleet/backend-develop:pr-123`
   - Applies to: `protofleet/backend-develop`, `protofleet/frontend-storybook-develop`

3. **Frontend Dashboard Pull Request Deployments**
   - Tag format: `pr-{number}-{flavor}`
   - Examples:
        - `protofleet/frontend-dashboard-develop:pr-123-main` - for flavor connected to the main develop backend
       - `protofleet/frontend-dashboard-develop:pr-123-pr` - for flavor connected to the PR backend
   - Applies to: `protofleet/frontend-dashboard-develop`

4. **Miner Simulator**
   - Tag format: `latest`
   - Example: `protofleet/miner-sim-develop:latest`
   - Applies to: `protofleet/miner-sim-develop`
   - The same image is deployed to all miner instances

### DNS Management (Route 53)

Route 53 is used to configure DNS records for our develop servers.

### IAM Configuration

#### GitHub Actions OIDC Integration

We use AWS IAM Identity Provider (IdP) for GitHub Actions authentication through OpenID Connect (OIDC).
The OIDC provider is configured for GitHub (`token.actions.githubusercontent.com`) and trusts our repository, enabling secure authentication without storing AWS credentials.

#### IAM Roles and Policies

1. **EC2 Instance Role** (`ProtoFleet_Develop_Server_EC2Role`)
    - Purpose: Grants required permissions for EC2 instances 
    - Attached Policies:
        - `ECR_ProtoFleetDevelop_Pull`: Allows pulling container images from ECR
        - `ProtoFleet_Develop_SessionManager`: Enables AWS Session Manager connectivity
        - `Route53_ProtoFleetDevelop_ACME-DNS-Challenge`: Permits Traefik to perform Let's Encrypt ACME DNS challenge validation

2. **GitHub Actions Deployment Role** (`GithubActions_ProtoFleet_Deploy_Develop`)
    - Purpose: Used by deployment workflows to manage infrastructure
    - Attached Policies:
        - `ECR_ProtoFleetDevelop_Deploy`: Grants permissions for:
            - Pushing images to ECR
        - `EC2InstanceConnect_ProtoFleetDevelop`: Grants permissions for:
            - EC2 Instance Connect access
            - SSM Session Manager access

3. **GitHub Actions Cleanup Role** (`GithubActions_ProtoFleet_Develop_CleanUp`)
    - Purpose: Used by cleanup workflows to remove unused ECR images
    - Attached Policies:
        - `ECR_ProtoFleetDevelop_CleanUp`: Allows:
            - Listing and describing ECR repositories and images
            - Deleting old/unused ECR images

## GitHub CI

### Actions

#### aws-ssh-setup

Sets AWS EC2 instance access (via the aws CMD) and creates several helper scripts that abstract some of the complexity of accessing the EC2 instance:

- `ssh_to_ec2`: For executing commands on the instance via ssh.
- `scp_to_ec2`: For file transfers using scp.
- `rsync_to_ec2`: For directory synchronization using rsync.

Each script handles ssh key management and tunnel setup automatically.

Those scripts also retry the command multiple times.
The retry is done primarily because the `ec2-instance-connect send-ssh-public-key` sometimes doesn't register the ssh key in time for the following commands.

##### Architecture

This action uses two AWS services in combination for secure, temporary SSH access:

1. **EC2 Instance Connect**:
    - This tool is used for two purposes:
        - Configuring temporary SSH public keys for the EC2 instances via the `send-ssh-public-key` command.
        - Opening a tunnel for the SSH connection via an aws instance connect endpoint.

2. **AWS Systems Manager (SSM)**:
    - This tool is used for registering the EC2 SSH host key so that the SSH can verify if it's connecting to the intended server.

Reasoning for this architecture:

- The primary goal of this setup is to provide a secure way for accessing the EC2 instance - using temporary credentials, with access managed by the IAM.
- SSM alone is cumbersome to use for executing simple scripts, and it does not support rsync/scp, making file copying difficult (requires using S3 or other workarounds).
- On the other hand, EC2 Instance Connect cannot safely provide SSH host key - it can only query the server using SSH which requires the host key.
    - Therefore, SSM is used for that purpose.
- The SSH tunnel using the instance connect endpoint is used so that we do not have to expose the SSH port to the public internet.

#### docker-compose-deploy

This workflow automates the deployment of Docker Compose applications to a remote server using SSH. This action:

- Stops any running containers from previous deployment (using `docker compose down`).
- Updates the docker-compose.yml file (in case it changed).
- Starts the updated Docker Compose deployment (using `docker compose up`).
- Cleans up unused Docker resources.

#### docker-image-pull

Handles pulling Docker images from AWS ECR to an EC2 instance. This action:

- Authenticates to AWS ECR from the EC2 instance.
- Pulls the specified Docker image from ECR.

#### docker-image-push

Manages pushing locally built Docker images to AWS ECR. This action:

- Authenticates to AWS ECR from the runner.
- Tags the Docker image appropriately for ECR.
- Pushes the image to the ECR repository.

#### docker-image-publish

A convenience action that combines `docker-image-push` and `docker-image-pull` operations, deploying the locally built Docker image to EC2 instance. 

### Workflows

#### Main Branch Develop Deployment

These workflows handle deployments for the main development environment, triggered by pushing to the `main` branch (merging a PR).

- **Develop Frontend** (`protofleet-deploy-develop-frontend.yml`):
    - Deploys frontend dashboard and storybook using the `protofleet-deploy-docker-to-develop-vm.yml` workflow.

- **Develop Backend** (`protofleet-deploy-develop-backend.yml`):
    - Deploys the backend + databases using the `protofleet-deploy-docker-to-develop-vm.yml` workflow.

#### PR Deployments

These workflows create isolated environments for testing changes and reviewing pull requests.

- **PR Frontend** (`protofleet-deploy-pr-frontend.yml`):
    - Runs if the PR modifies frontend code.
    - Deploys frontend dashboard and storybook using the `protofleet-deploy-docker-to-develop-vm.yml` workflow.
    - The deployed dashboard is connected to the main develop backend so that it uses the same data as the main develop frontend for easy comparison of changes.
    - Uses `protofleet-report-pr-deployment.yml` for adding comments to the PR with links to the deployed environment.

- **PR Backend** (`protofleet-deploy-pr-backend.yml`):
    - Runs if the PR modifies backend code.
    - Deploys backend and a frontend dashboard using the `protofleet-deploy-docker-to-develop-vm.yml` workflow.
    - The deployed dashboard is connected to the backend deployed for this PR (not to the main develop backend as is the case for the PR Frontend deployment).
    - Uses `protofleet-report-pr-deployment.yml` for adding comments to the PR with links to the deployed environment.

#### Infrastructure Deployments

- **Traefik** (`protofleet-deploy-develop-traefik.yml`):
    - Deploys Traefik and Portainer to all VMs
    - This workflow is triggered primarily by pushing updates to `main`
    - However, it can be triggered manually if needed for testing those changes in a PR

- **Miner Simulator** (`protofleet-deploy-miner-simulator.yml`):
    - Deploys 3 miner simulators for testing ProtoFleet
    - This workflow is triggered only manually.

#### Maintenance Workflows

- **Deploy Cleanup** (`protofleet-deploy-clean-up.yml`):
    - Runs daily (overnight) to free unused resources
    - Stops and deletes containers (and associated resources) deployed for closed and stale PRs
    - Deletes unused images from ECR

- **Reset Database** (`protofleet-reset-database.yml`):
    - Used to reset backend databases without having to use SSH
    - This workflow is triggered only manually
    - It provides the option to choose which of the two databases (mysql and influxdb) to reset
    - It also provides the option to select which environment to reset (main branch deployment vs. a PR deployment)

#### Sub-workflows

Supporting workflows that are called by other workflows to perform a common task.

- **Docker VM Deployment** (`protofleet-deploy-docker-to-develop-vm.yml`):
    - Core deployment workflow used by most workflows which deploy containers to an EC2 instance
    - It can be used if the deployment requires these exact steps:
        - Building locally a Docker image based on a Dockerfile
        - Publishing this image to the EC2 instance (via ECR)
        - Substituting parameters in Docker Compose file
          - Parameters:
            - `VM_SUBDOMAIN`: Replaced with the target environment subdomain
            - `LABEL`: Replaced with service identifier (PR number or 'develop')
            - `IMAGE`: Replaced with appropriate Docker image reference
          - These parameters are declared as Docker Compose environment variables, but are substituted using find/replace. 
            - This substitution makes it easier to work with the deployed Docker compose file from the EC2 instance console.
        - Deploying the containers using the previously modified Docker Compose file

- **PR Deployment Report** (`protofleet-report-pr-deployment.yml`):
    - Used by PR deployments to create a comment in the PR notifying users about which services were deployed and what is their URL

## Deployed Services

All services are deployed under the `fleetdev.proto.xyz` domain.

### Infrastructure Services

Docker Compose location: `deploy/develop/traefik/`

- **Traefik Dashboard**: `https://traefik.{vm-subdomain}.fleetdev.proto.xyz`
  - Reverse proxy for all services, provides:
    - SSL termination
    - SSL certificate management via Let's Encrypt and Route53 DNS challenge
    - Request routing with automatic Docker container discovery
  - The URL above leads to Traefik's dashboard
  - Public services are connected through the `protofleet-develop-traefik_traefik` network with Traefik
  - Public ports:
      - 80: HTTP (redirects to HTTPS)
      - 443: HTTPS
      - 2121: gRPC (with TLS)
  - It is deployed to all AWS EC2 instances, each with a unique `{vm-subdomain}`:
    - `develop`
      - This VM hosts everything except for the Miner simulators
    - `miner-1.miner`, `miner-2.miner` and `miner-3.miner`
      - Miner simulators

- **Portainer**: `https://portainer.{vm-subdomain}.fleetdev.proto.xyz`
  - Container management interface - allows to see container status, logs, etc. without having to use SSH
  - Uses the same `{vm-subdomain}` as Traefik and is also deployed to all VMs.

### Backend

Docker Compose location: `deploy/develop/backend/`

- **Backend API**: `https://backend.{label}.develop.fleetdev.proto.xyz`
  - Provides gRPC API for frontend dashboard on port 443
  - The `{label}` parameter follows the same pattern as for ProtoFleet Dashboard

- **MySQL Database**:
  - Used by the backend
  - Not exposed externally

- **InfluxDB Time Series Database**:
  - Used by the backend
  - Not exposed externally

### Frontend

- **ProtoFleet Dashboard**: `https://dashboard.{label}.develop.fleetdev.proto.xyz`
  - Provides the main ProtoFleet web interface on port 443
  - Docker Compose location: `deploy/develop/frontend/dashboard/`
  - The `{label}` parameter is:
    - `main` - for main branch deployments
    - `pr-{number}` - for PR deployments

- **Storybook**: `https://storybook.{label}.develop.fleetdev.proto.xyz`
  - Provides UI component documentation on port 443
  - Docker Compose location: `deploy/develop/frontend/storybook/`
  - The `{label}` parameter follows the same pattern as ProtoFleet Dashboard

### Miner Simulator

- **Miner Simulator**: `https://{miner-name}.miner.fleetdev.proto.xyz`
  - Provides a miner simulator for development and testing so that we do not have to rely on having access to real miners.
  - Exposed services:
    - ProtoOS UI and API on port 443
    - gRPC API on port 2121
  - The `{miner-name}` is:
    - `miner-1`, `miner-2` or `miner-3`
  - Based on two Docker images:
      - Base image from `miner-firmware/docker/sim/`: Declared in the Miner firmware repo and is the base of the simulator
      - Our extended version `deploy/develop/miner/`: Uses the base image and adds the ProtoOS web interface to it
  - Each miner has a corresponding serial number: `develop-miner-1`, `develop-miner-2`, `develop-miner-3`
  - Each miner simulator is deployed to a dedicated AWS instance
