# Docker Compose Configuration

This project uses a consolidated Docker Compose setup with a base configuration file and environment-specific override files.

## Structure

- `server/docker-compose.base.yaml` - Contains common configurations shared between all environments
- `server/docker-compose.yaml` - Development environment configuration
- `deployment-files/docker-compose.yaml` - Production/deployment environment configuration

## How It Works

The setup uses Docker Compose's extends feature to inherit common configurations from the base file while allowing environment-specific overrides.

### Base Configuration

The base configuration (`server/docker-compose.base.yaml`) includes:

- Common service definitions
- Shared environment variables
- Volume definitions
- Network configuration
- Health checks

### Environment-Specific Configurations

Each environment extends the base configuration and adds or overrides settings as needed:

#### Development (`server/docker-compose.yaml`)

- Development-specific build contexts
- Local development settings
- Testing services (mms, fake-antminer)
- Development-specific network configuration

#### Production (`deployment-files/docker-compose.yaml`)

- Production build contexts
- Environment variable interpolation using `${VARIABLE}` syntax
- Production-specific services
- Client front-end service

## Extending Configuration

To add a new service:

1. If the service is common to all environments, add it to `server/docker-compose.base.yaml`
2. If the service is specific to an environment, add it to the respective environment's compose file
3. If a service needs to extend the base configuration, use the `extends` key:

```yaml
services:
  my-service:
    extends:
      file: ./docker-compose.base.yaml
      service: base-service
    # Add environment-specific overrides here
```

## Maintenance

When making changes:

1. Common changes should go in the base file
2. Environment-specific changes should go in the respective environment file
3. Test changes in both environments to ensure proper inheritance

## Design Considerations

### Service Dependencies

Due to a limitation in Docker Compose's extension mechanism, services with `depends_on` directives cannot be extended. To work around this:

1. The base configuration file (`docker-compose.base.yaml`) does not contain any `depends_on` directives
2. Each environment-specific file adds the necessary `depends_on` directives for its services

This approach allows services to be extended while maintaining proper dependency relationships in each environment.
