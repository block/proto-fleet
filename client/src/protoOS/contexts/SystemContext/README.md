# SystemContext

A centralized React context for managing system information across the entire application with efficient polling and data consistency.

## Overview

The `SystemContext` provides a single source of truth for system information, preventing multiple polling instances and ensuring all components share the same data. It wraps the `useSystemInfo` hook and provides centralized control over polling behavior.

## Features

- ✅ **Single Polling Instance**: Only one system info polling instance across the entire app
- ✅ **Prevents Parallel Calls**: Uses ref-based approach to prevent simultaneous API calls
- ✅ **Consistent Data**: All components share the same system info state
- ✅ **Configurable Polling**: Centralized control over polling interval and behavior
- ✅ **Type Safety**: Full TypeScript support with proper type definitions

## Usage

### Basic Usage

```tsx
import { useSystemContext } from "@/protoOS/contexts/SystemContext";

const MyComponent = () => {
  const { data: systemInfo, pending, error, reload } = useSystemContext();

  if (pending) {
    return <div>Loading system info...</div>;
  }

  if (error) {
    return <div>Error: {error}</div>;
  }

  return (
    <div>
      <h2>System Information</h2>
      <p>Web Server: {systemInfo?.web_server_status}</p>
      <p>Mining Driver: {systemInfo?.mining_driver_sw?.name}</p>
      <button onClick={reload}>Refresh</button>
    </div>
  );
};
```

### Using Processed Data

The context also provides processed data for common system states:

```tsx
import { useSystemContext } from "@/protoOS/contexts/SystemContext";

const SystemStatus = () => {
  const { processedData } = useSystemContext();

  return (
    <div>
      <div>
        Web Server: {processedData?.isWebServerRunning ? "Running" : "Stopped"}
      </div>
      <div>
        Mining Driver:{" "}
        {processedData?.isMiningDriverRunning ? "Running" : "Stopped"}
      </div>
      <div>
        Firmware Update:{" "}
        {processedData?.hasFirmwareUpdate ? "Available" : "Up to date"}
      </div>
    </div>
  );
};
```

### Manual Refresh

```tsx
import { useSystemContext } from "@/protoOS/contexts/SystemContext";

const RefreshButton = () => {
  const { reload, pending } = useSystemContext();

  return (
    <button onClick={reload} disabled={pending}>
      {pending ? "Refreshing..." : "Refresh System Info"}
    </button>
  );
};
```

## API Reference

### useSystemContext()

Returns the system context value with the following properties:

| Property        | Type                                | Description                                |
| --------------- | ----------------------------------- | ------------------------------------------ |
| `data`          | `SystemInfoSysteminfo \| undefined` | Raw system info data from the API          |
| `processedData` | `ProcessedSystemInfo \| undefined`  | Processed system info with boolean flags   |
| `pending`       | `boolean`                           | Whether a request is currently in progress |
| `error`         | `string \| undefined`               | Error message if the request failed        |
| `reload`        | `() => void`                        | Function to manually trigger a refresh     |

### ProcessedSystemInfo

```typescript
interface ProcessedSystemInfo {
  isWebServerRunning: boolean; // Whether the miner API server is running
  isMiningDriverRunning: boolean; // Whether the MCDD (Mining Driver) is running
  hasFirmwareUpdate: boolean; // Whether a firmware update is available
}
```

## Configuration

The `SystemContextProvider` is configured in `main.tsx`:

```tsx
<SystemContextProvider poll={true} pollIntervalMs={35 * 1000}>
  {/* Your app */}
</SystemContextProvider>
```

### Provider Props

| Prop             | Type      | Default | Description                         |
| ---------------- | --------- | ------- | ----------------------------------- |
| `poll`           | `boolean` | `true`  | Whether to enable automatic polling |
| `pollIntervalMs` | `number`  | `10000` | Polling interval in milliseconds    |

## Integration

The `SystemContextProvider` is already integrated into the app in `main.tsx`:

```tsx
const Main = () => {
  return (
    <MinerHostingProvider>
      <AuthProvider>
        <SystemContextProvider>
          <RouterProvider router={router} />
        </SystemContextProvider>
      </AuthProvider>
    </MinerHostingProvider>
  );
};
```

## Migration from useSystemInfo

If you're migrating from the old `useSystemInfo` hook:

### Before

```tsx
import { useSystemInfo } from "@/protoOS/api/useSystemInfo";

const MyComponent = () => {
  const {
    data: systemInfo,
    pending,
    error,
    reload,
  } = useSystemInfo({ poll: true });
  // This creates a separate polling instance
};
```

### After

```tsx
import { useSystemContext } from "@/protoOS/contexts/SystemContext";

const MyComponent = () => {
  const { data: systemInfo, pending, error, reload } = useSystemContext();
  // This uses the centralized polling instance
};
```

## Benefits

1. **Performance**: Eliminates multiple polling instances and reduces API calls
2. **Consistency**: All components share the same system info data
3. **Reliability**: Prevents race conditions and parallel API calls
4. **Maintainability**: Centralized control over polling behavior
5. **Developer Experience**: Simple API with full TypeScript support

## Best Practices

1. **Use processedData for UI logic**: The processed data provides boolean flags that are easier to work with in components
2. **Handle loading states**: Always check the `pending` state before rendering data
3. **Error handling**: Provide fallback UI when `error` is present
4. **Avoid manual polling**: Let the context handle polling automatically unless you need immediate refresh
5. **Type safety**: Use TypeScript to get full type checking for the context values

## Troubleshooting

### Context not available

If you get an error about the context not being available, ensure your component is wrapped within the `SystemContextProvider` in the component tree.

### Data not updating

- Check that polling is enabled (`poll={true}`)
- Verify the polling interval is appropriate for your use case
- Use the `reload()` function for immediate refresh if needed

### Performance issues

- The context is optimized to prevent unnecessary re-renders
- If you need to optimize further, consider using `React.memo` for components that only depend on specific parts of the system info
