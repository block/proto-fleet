# InfluxDB Telemetry Store - Comprehensive Development Guide

This document provides complete guidance for AI models and developers working on the InfluxDB telemetry store package. It covers architecture, patterns, conventions, and critical implementation details.

**📋 IMPORTANT NOTE FOR AI AGENTS**: Only update this context.md file when introducing genuinely new patterns or architectural changes. Most development work should follow existing patterns exactly without requiring documentation updates. See the "Updating This Context.md File" section for detailed guidance on when updates are needed.

## Package Overview

The InfluxDB package implements a telemetry data store using InfluxDB v3-core, providing persistent storage for Bitcoin mining device telemetry data. It follows clean architecture principles with clear separation between domain logic and infrastructure concerns.

### Key Responsibilities
- **Data Persistence**: Store telemetry data from mining devices
- **Query Operations**: Retrieve latest, time-series, and aggregated telemetry
- **Metadata Management**: Track device metadata and status
- **Real-time Streaming**: Provide live telemetry updates via polling
- **Error Handling**: Comprehensive error types with retry logic
- **Testing Infrastructure**: Full integration test support with containerized InfluxDB

## Architecture Overview

```
Domain Layer (external)
├── telemetry.TelemetryDataStore interface
└── models.* (Telemetry, DeviceMetadata, etc.)
                    ↓
Infrastructure Layer (this package)
├── InfluxTelemetryStore (main implementation)
├── Config (configuration and validation)
├── Error Types (comprehensive error handling)
├── Models Package (data transformation)
└── TestUtils (integration testing)
```

## File Structure and Responsibilities

```
influxdb/
├── context.md                    # This comprehensive guide
├── config.go                     # Configuration types and validation
├── errors.go                     # Error types and retry logic
├── telemetry_store.go            # Main store implementation
├── telemetry_store_test.go       # Comprehensive test suite
├── models/
│   ├── point_mapping.go          # Domain ↔ InfluxDB conversions
│   └── query_results.go          # Query result mapping utilities
└── testutils/
    └── container.go              # InfluxDB v3-core container setup
```

### File-by-File Breakdown

#### `config.go`
- **Configuration struct** with validation tags
- **Environment variable mapping** for 12-factor app compliance
- **Validation function** with detailed error messages
- **Default values** for timeouts and retry settings

#### `errors.go`
- **Comprehensive error types** for different failure scenarios
- **Retry logic** with exponential backoff and overflow protection
- **Error constructors** for consistent error creation
- **Legacy compatibility** wrappers for backward compatibility

#### `telemetry_store.go`
- **Main store implementation** with all CRUD operations
- **Embedded SQL queries** defined immediately above methods
- **Retry logic** with configurable attempts and delays
- **Structured logging** with slog for observability
- **Resource management** with proper cleanup

#### `models/point_mapping.go`
- **Bidirectional conversion** between domain and InfluxDB types
- **Type-safe transformations** with proper field mapping
- **Timestamp handling** for time-series data

#### `models/query_results.go`
- **Query result mapping** for complex aggregations
- **Device metadata extraction** from InfluxDB responses
- **Measurement type parsing** with enum conversion
- **Flexible result handling** for various query types

#### `testutils/container.go`
- **InfluxDB v3-core container setup** for integration tests
- **Token management** with admin token creation
- **Database initialization** with proper authentication
- **Health check integration** for reliable test startup

## Core Implementation Patterns

### 1. Query Definition Pattern

**✅ ALWAYS follow this pattern:**
```go
// Method comment describing purpose and behavior
const methodNameQuery = `SELECT device_id, time, *, '%s' as measurement
FROM %s
WHERE device_id IN ($device_ids)
AND time >= $start_time
ORDER BY time DESC
LIMIT $limit`

func (s *InfluxTelemetryStore) MethodName(ctx context.Context, query models.QueryType) ([]models.ResultType, error) {
    // Note: First %s is for measurement alias, second %s is for measurement_type in FROM
    sqlQuery := fmt.Sprintf(methodNameQuery, query.MeasurementType, query.MeasurementType)
    params := s.getMethodNameParams(query)
    
    iterator, err := s.client.QueryPointValueWithParameters(ctx, sqlQuery, params)
    if err != nil {
        return nil, newTelemetryQueryError(err, "MethodName")
    }
    
    var results []models.ResultType
    var iterationErrors []error
    successCount := 0
    
    for point, err := iterator.Next(); err != influxdb3.Done; point, err = iterator.Next() {
        if err != nil {
            s.logger.Error("error reading point in MethodName", slog.Any("error", err))
            iterationErrors = append(iterationErrors, err)
            continue
        }
        result := influxModels.ToResultType(point)
        results = append(results, result)
        successCount++
    }
    
    // Handle partial success scenarios
    if len(iterationErrors) > 0 {
        if successCount > 0 {
            s.logger.Warn("MethodName completed with partial data",
                slog.Int("success_count", successCount),
                slog.Int("error_count", len(iterationErrors)))
        } else {
            return nil, newTelemetryIterationError(iterationErrors[0], "MethodName", len(iterationErrors), successCount > 0)
        }
    }
    
    return results, nil
}
```

**Key Elements:**
- Query constant named `methodNameQuery`
- Query defined immediately above method
- Proper error handling with custom error types
- Partial success handling with logging
- Structured logging with method context

### 2. Parameter Builder Pattern

```go
func (s *InfluxTelemetryStore) getMethodNameParams(query models.QueryType) influxdb3.QueryParameters {
    params := make(influxdb3.QueryParameters)
    params["device_ids"] = deviceIDsToStrings(query.DeviceIDs)
    params["measurement_types"] = measurementTypesToStrings(query.MeasurementTypes)
    
    // Handle optional parameters with sensible defaults
    if query.TimeRange.StartTime != nil {
        params["start_time"] = *query.TimeRange.StartTime
    } else {
        params["start_time"] = time.Now().Add(-24 * time.Hour)
    }
    
    if query.Limit != nil {
        params["limit"] = *query.Limit
    } else {
        params["limit"] = 1000
    }
    
    return params
}
```

### 3. Error Handling Pattern

**Custom Error Types:**
```go
// Use specific error constructors
return nil, newTelemetryQueryError(err, "MethodName")
return nil, newTelemetryWriteError(err, pointCount)
return nil, newTelemetryConnectionError(err)
```

**Retry Logic Pattern:**
```go
baseDelay := s.config.RetryDelay
maxAttempts := s.config.RetryAttempts
if maxAttempts <= 0 {
    maxAttempts = 3 // Default to 3 attempts if not configured
}
if baseDelay <= 0 {
    baseDelay = 100 * time.Millisecond // Default delay
}

for attempt := range maxAttempts {
    err = s.client.WritePoints(ctx, points)
    if err == nil {
        return nil // Success
    }
    
    if !isRetryableError(err) {
        return newTelemetryWriteError(err, len(points))
    }
    
    // Don't sleep after the last attempt
    if attempt < maxAttempts-1 {
        if attempt > 30 { // Prevent overflow for very large attempt numbers
            attempt = 30
        }
        multiplier := 1 << attempt // 2^attempt
        delay := time.Duration(int64(baseDelay) * int64(multiplier))
        
        select {
        case <-ctx.Done():
            return newTelemetryWriteError(ctx.Err(), len(points))
        case <-time.After(delay):
        }
    }
}
```

### 4. Logging Pattern

```go
// Use structured logging with context
s.logger.Debug("operation succeeded",
    slog.String("operation", "MethodName"),
    slog.Int("result_count", len(results)),
    slog.Int64("duration_ms", duration.Milliseconds()))

s.logger.Error("operation failed",
    slog.String("operation", "MethodName"),
    slog.Any("error", err),
    slog.Int("attempt", attempt))

s.logger.Warn("partial success",
    slog.String("operation", "MethodName"),
    slog.Int("success_count", successCount),
    slog.Int("error_count", errorCount))
```

## Data Transformation Patterns

### Domain to InfluxDB Conversion

```go
func ToInfluxPoint(telemetry models.Telemetry) *influxdb3.Point {
    point := influxdb3.NewPointWithMeasurement(telemetry.Measurement).
        SetTimestamp(telemetry.Timestamp)

    // Add all tags
    for key, value := range telemetry.Tags {
        point.SetTag(key, value)
    }

    // Add all fields
    for key, value := range telemetry.Fields {
        point.SetField(key, value)
    }

    return point
}
```

### InfluxDB to Domain Conversion

```go
func ToTelemetry(pv *influxdb3.PointValues) models.Telemetry {
    // Extract fields
    fields := pv.Fields
    for _, fieldName := range pv.GetFieldNames() {
        fields[fieldName] = pv.GetField(fieldName)
    }

    // Extract tags
    tags := make(map[string]string)
    for _, tagName := range pv.GetTagNames() {
        if tagValue, exists := pv.GetTag(tagName); exists {
            tags[tagName] = tagValue
        }
    }

    return models.Telemetry{
        Measurement: pv.GetMeasurement(),
        Fields:      fields,
        Tags:        tags,
        Timestamp:   pv.Timestamp,
    }
}
```

## Configuration Management

### Configuration Structure

```go
type Config struct {
    // Required fields with validation
    URL          string `json:"url" validate:"required,url" env:"INFLUX_URL"`
    Organization string `json:"organization" validate:"required" env:"INFLUX_ORG"`
    Bucket       string `json:"bucket" validate:"required" env:"INFLUX_BUCKET"`
    Token        string `json:"token" validate:"required" env:"INFLUX_TOKEN"`

    // Optional fields with defaults
    WriteTimeout  time.Duration `json:"write_timeout" default:"30s"`
    QueryTimeout  time.Duration `json:"query_timeout" default:"60s"`
    BatchSize     int           `json:"batch_size" default:"1000"`
    FlushInterval time.Duration `json:"flush_interval" default:"5s"`
    RetryAttempts int           `json:"retry_attempts" default:"3"`
    RetryDelay    time.Duration `json:"retry_delay" default:"100ms"`
}
```

### Validation Pattern

```go
func validateConfig(config Config) error {
    if config.URL == "" {
        return fmt.Errorf("URL is required")
    }

    if _, err := url.Parse(config.URL); err != nil {
        return fmt.Errorf("invalid URL format: %w", err)
    }

    // Validate all required fields with specific error messages
    if config.Organization == "" {
        return fmt.Errorf("organization is required")
    }

    return nil
}
```

## Critical Testing Principles

### ❌ **NEVER Use Graceful Test Failures**

**Tests should NEVER fail gracefully or skip due to implementation issues.** This is a fundamental testing principle that must be strictly followed.

#### **❌ Bad Pattern - Graceful Degradation:**
```go
// WRONG - This masks real bugs
results, err := store.GetLatestDeviceMetricsBatch(ctx, deviceIDs)
if err != nil {
    t.Logf("Query failed: %v", err)
    t.Skip("InfluxDB query issues - skipping detailed assertions")
    return
}
```

#### **✅ Correct Pattern - Fail Fast:**
```go
// CORRECT - This catches real bugs
results, err := store.GetLatestDeviceMetricsBatch(ctx, deviceIDs)
require.NoError(t, err, "GetLatestDeviceMetricsBatch should succeed - if this fails, there's a bug in the implementation")
```

#### **Why This Matters:**
1. **Masks Real Bugs**: Graceful failures hide implementation issues that need to be fixed
2. **False Confidence**: Passing tests don't actually verify the functionality works
3. **Technical Debt**: Problems accumulate and become harder to fix later
4. **Production Risk**: Issues that are skipped in tests will likely fail in production

#### **When Tests Should Skip:**
- **Only for external dependencies** that are genuinely unavailable (e.g., Docker not installed)
- **Never for implementation logic** - if the code should work, the test should verify it works

#### **"Invalid ticket" Errors Are Implementation Bugs:**
**🚨 CRITICAL**: If you see "Invalid ticket" errors in InfluxDB v3-core tests, this indicates a bug in the query implementation, NOT an environment issue. Common causes:

1. **Wrong SQL syntax** - Using SQL instead of InfluxQL
2. **Incorrect FROM clause** - Using bucket name instead of measurement_type
3. **Unsupported operators** - Using ANY() instead of IN()
4. **Missing measurement alias** - Not including measurement name in SELECT

**Example of Real Bug Hidden by Graceful Failure:**
The original test was skipping query failures with "Invalid ticket" errors, which hid the fact that the InfluxDB v3-core queries were using incorrect InfluxQL syntax. The authentication worked fine, writes worked fine, but queries were broken due to wrong query structure. This should have been caught immediately, not gracefully skipped.

**✅ Correct Response to "Invalid ticket" Errors:**
1. **Fix the query implementation** - Don't skip the test
2. **Check InfluxQL syntax** - Ensure proper FROM clause and operators
3. **Verify measurement names** - Use actual measurement_type, not bucket
4. **Test the fix** - Ensure the query works correctly

## Testing Patterns and Guidelines

### Test Categories

1. **Unit Tests** - Fast, no external dependencies
2. **Integration Tests** - Real InfluxDB containers
3. **Configuration Tests** - Validation and error handling

### Unit Test Pattern

```go
func TestMethodName(t *testing.T) {
    tests := []struct {
        name        string
        input       InputType
        expectError bool
        errorMsg    string
        expected    ExpectedType
    }{
        {
            name:        "valid input",
            input:       validInput,
            expectError: false,
            expected:    expectedOutput,
        },
        {
            name:        "invalid input",
            input:       invalidInput,
            expectError: true,
            errorMsg:    "expected error message",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := methodUnderTest(tt.input)
            
            if tt.expectError {
                require.Error(t, err)
                assert.Contains(t, err.Error(), tt.errorMsg)
            } else {
                require.NoError(t, err)
                assert.Equal(t, tt.expected, result)
            }
        })
    }
}
```

### Integration Test Pattern

**✅ ALWAYS use this exact pattern:**

```go
func TestInfluxTelemetryStore_MethodName(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }

    t.Parallel() // Enable parallel execution

    // Setup InfluxDB container
    container, testConfig := testutils.SetupInfluxDBContainer(t)
    defer func() {
        if err := container.Terminate(t.Context()); err != nil {
            t.Logf("Failed to terminate container: %v", err)
        }
    }()

    // Create store configuration
    config := Config{
        URL:           testConfig.URL,
        Organization:  testConfig.Organization,
        Bucket:        testConfig.Bucket,
        Token:         testConfig.Token,
        WriteTimeout:  testConfig.WriteTimeout,
        QueryTimeout:  testConfig.QueryTimeout,
        RetryAttempts: 3,
        RetryDelay:    50 * time.Millisecond,
    }

    // Create store
    store, err := NewTelemetryStore(config)
    require.NoError(t, err)
    defer store.Close()

    ctx := t.Context()

    // Test connection
    err = store.Ping(ctx)
    require.NoError(t, err, "Should be able to ping InfluxDB")

    // Your test logic here
    testData := createTestData()
    
    err = store.MethodName(ctx, testData)
    require.NoError(t, err, "Should successfully execute method")
    
    // Verify results
    // ...
}
```

### Test Data Helpers

```go
func createTestTelemetry(deviceID, measurement string, value float64) models.Telemetry {
    return models.Telemetry{
        Measurement: measurement,
        Fields: map[string]any{
            "value": value,
        },
        Tags: map[string]string{
            "device_id": deviceID,
            "test":      "integration",
        },
        Timestamp: time.Now(),
    }
}

func createTestTelemetryWithTimestamp(deviceID, measurement string, value float64, timestamp time.Time) models.Telemetry {
    return models.Telemetry{
        Measurement: measurement,
        Fields: map[string]any{
            "value": value,
        },
        Tags: map[string]string{
            "device_id": deviceID,
            "test":      "integration",
        },
        Timestamp: timestamp,
    }
}
```

### Container Setup Details

The `testutils.SetupInfluxDBContainer()` function provides:

- **InfluxDB v3-core** container with proper configuration
- **Random port allocation** for parallel test execution
- **Health check integration** - no arbitrary sleeps needed
- **Admin token creation** for authentication
- **Database initialization** ready for immediate use
- **Proper cleanup** with container termination

**Configuration returned:**
```go
type Config struct {
    URL          string        // http://localhost:randomPort
    Organization string        // "testorg"
    Bucket       string        // "testbucket"
    Token        string        // Generated admin token
    WriteTimeout time.Duration // 30 seconds
    QueryTimeout time.Duration // 60 seconds
}
```

### Integration Test Best Practices

#### ✅ Do:
- **Use `t.Parallel()`** for all integration tests to enable parallel execution
- **Use `testutils.SetupInfluxDBContainer(t)`** for container setup
- **Skip in short mode**: `if testing.Short() { t.Skip("...") }`
- **Use `t.Context()`** instead of `context.Background()`
- **Defer container cleanup**: Always terminate containers
- **Test real functionality**: Store, retrieve, ping operations
- **Use descriptive test names**: `TestInfluxTelemetryStore_Store_Data`

#### ❌ Don't:
- Don't use `time.Sleep()` - the health check handles readiness
- Don't hardcode ports - use random port allocation
- Don't skip cleanup - always defer `container.Terminate()`
- Don't test without Docker available - tests will fail gracefully
- Don't mix unit and integration test logic
- Don't forget `t.Parallel()` - it enables faster test execution

## SQL Query Guidelines

### InfluxQL Requirements

**🚨 CRITICAL: All query strings must be written in InfluxQL syntax, not SQL.**

**Key InfluxQL Requirements:**
- **FROM clause**: Use `measurement_type` (the actual measurement name), NOT the bucket name
- **Single measurement per query**: Unless joining, query only one measurement_type per query
- **MeasurementName population**: To populate the `PointValue.MeasurementName` field, the SELECT statement must include `'measurement_type' as measurement`
- **Parameter handling**: You CANNOT pass lists directly as parameters to `QueryPointValueWithParameters`. Lists must be populated in the query string before calling the method
- **SELECT clause**: You CANNOT mix `*` with explicit field names in SELECT. Use either `SELECT *` OR `SELECT field1, field2, ...` but never both
- **Device ID lists**: Use the `buildDeviceIDsString()` method to safely construct device ID lists in queries
- **IN operator**: Use `IN (%s)` in the query template and populate the constructed string via `fmt.Sprintf`, not `IN ($parameter)`

### Query Structure Standards

```sql
-- InfluxQL format - Use measurement_type in FROM, not bucket
-- Populate device IDs in query string using fmt.Sprintf, don't use parameters for lists
SELECT device_id, time, value, 'measurement_type' as measurement
FROM measurement_type
WHERE device_id IN (%s)
  AND time >= $start_time
  AND time <= $end_time
ORDER BY time ASC
LIMIT $limit
```

**❌ Wrong - Using parameters for lists and mixing * with fields:**
```sql
SELECT device_id, time, *, 'measurement' as measurement  -- WRONG: mixing * with fields
FROM measurement_type
WHERE device_id IN ($device_ids)  -- WRONG: can't pass list as parameter
```

**✅ Correct - Populate lists in query string using fmt.Sprintf, use either * or specific fields:**
```sql
-- Option 1: Use specific fields with IN (%s) populated via fmt.Sprintf
SELECT device_id, time, value, 'power_consumption' as measurement
FROM power_consumption
WHERE device_id IN (%s)  -- %s will be populated with buildDeviceIDsString() result
  AND time >= $start_time

-- Option 2: Use * (but then you can't add measurement alias easily)
SELECT *
FROM power_consumption
WHERE device_id IN (%s)  -- %s will be populated with buildDeviceIDsString() result
  AND time >= $start_time
```

### Parameter Usage Guidelines

**✅ Safe to use as parameters:**
- Single scalar values: `$start_time`, `$end_time`, `$limit`
- Single strings: `$device_type`, `$location`

**❌ Cannot use as parameters:**
- Lists/arrays: `$device_ids`, `$measurement_types`
- Complex expressions

**✅ Correct approach for lists:**
```go
// Build device IDs string safely
deviceIDsStr := s.buildDeviceIDsString(query.DeviceIDs)

// Use IN (%s) in query template and populate via fmt.Sprintf
const queryTemplate = `SELECT device_id, time, value, '%s' as measurement
FROM %s
WHERE device_id IN (%s)
AND time >= $max_age`

// Populate the query string with measurement name, measurement name, and device IDs
sqlQuery := fmt.Sprintf(queryTemplate, measurementName, measurementName, deviceIDsStr)

// Only pass scalar parameters to QueryPointValueWithParameters
params := make(influxdb3.QueryParameters)
params["max_age"] = time.Now().Add(-maxAge)

iterator, err := s.client.QueryPointValueWithParameters(ctx, sqlQuery, params)
```

### Parameter Naming Conventions

- `$start_time`, `$end_time` - Time range boundaries (safe as parameters)
- `$limit` - Maximum number of results (safe as parameter)
- `$last_timestamp` - For streaming/polling queries (safe as parameter)
- `$max_age` - Relative time constraint (safe as parameter)

**Note**: Device IDs and measurement types must be populated in the query string using helper methods and `fmt.Sprintf`, not passed as parameters. Use `IN (%s)` in the query template and populate the `%s` with the constructed string.

## Error Handling Comprehensive Guide

### Error Type Hierarchy

```go
// Main error types
type InfluxTelemetryError struct {
    Operation    string                 // Method name
    ErrorType    TelemetryErrorType    // Categorized error type
    Cause        error                 // Original error
    Context      map[string]interface{} // Additional context
    PartialData  bool                  // Whether some data succeeded
    ErrorCount   int                   // Number of errors in batch
    RetryAttempt int                   // Which retry attempt failed
}

// Error categories
const (
    TelemetryErrorTypeUnknown TelemetryErrorType = iota
    TelemetryErrorTypeConnection
    TelemetryErrorTypeConfig
    TelemetryErrorTypeQuery
    TelemetryErrorTypeWrite
    TelemetryErrorTypeIteration
    TelemetryErrorTypeDataConversion
    TelemetryErrorTypeClose
    TelemetryErrorTypePing
)
```

### Error Constructor Usage

```go
// For query errors
return nil, newTelemetryQueryError(err, "GetLatestDeviceMetricsBatch")

// For write errors
return newTelemetryWriteError(err, pointCount)

// For write errors with retry context
return newTelemetryWriteErrorWithRetry(err, pointCount, retryAttempt)

// For connection errors
return nil, newTelemetryConnectionError(err)

// For iteration errors with partial data
return nil, newTelemetryIterationError(err, "GetLatestDeviceMetricsBatch", errorCount, hasPartialData)
```

### Retry Logic Implementation

```go
func isRetryableError(err error) bool {
    if err == nil {
        return false
    }

    // Network errors are generally retryable
    if netErr, ok := err.(net.Error); ok {
        return netErr.Timeout() || netErr.Temporary()
    }

    // Connection errors
    if opErr, ok := err.(*net.OpError); ok {
        return true
    }

    // System call errors that are retryable
    if err == syscall.ECONNREFUSED || err == syscall.ECONNRESET || err == syscall.ETIMEDOUT {
        return true
    }

    // String-based error detection for InfluxDB-specific errors
    errStr := strings.ToLower(err.Error())
    retryableStrings := []string{
        "connection refused",
        "connection reset",
        "timeout",
        "temporary failure",
        "service unavailable",
        "too many requests",
        "rate limit",
        "server error",
        "internal server error",
        "bad gateway",
        "gateway timeout",
        "network is unreachable",
        "no route to host",
    }

    for _, retryableStr := range retryableStrings {
        if strings.Contains(errStr, retryableStr) {
            return true
        }
    }

    return false
}
```

## Adding New Store Methods - Step-by-Step Guide

### 1. Define the Query Constant

```go
const getNewDataQuery = `SELECT device_id, time, *, '%s' as measurement
FROM %s
WHERE device_id IN ($device_ids)
AND time >= $start_time
ORDER BY time DESC
LIMIT $limit`
```

### 2. Implement the Main Method

```go
func (s *InfluxTelemetryStore) GetNewData(ctx context.Context, query models.NewDataQuery) ([]models.NewDataResult, error) {
    // Note: Query single measurement_type - first %s for alias, second for FROM clause
    sqlQuery := fmt.Sprintf(getNewDataQuery, query.MeasurementType, query.MeasurementType)
    params := s.getNewDataParams(query)

    iterator, err := s.client.QueryPointValueWithParameters(ctx, sqlQuery, params)
    if err != nil {
        return nil, newTelemetryQueryError(err, "GetNewData")
    }

    var results []models.NewDataResult
    var iterationErrors []error
    successCount := 0

    for point, err := iterator.Next(); err != influxdb3.Done; point, err = iterator.Next() {
        if err != nil {
            s.logger.Error("error reading point in GetNewData", slog.Any("error", err))
            iterationErrors = append(iterationErrors, err)
            continue
        }
        result := influxModels.ToNewDataResult(point)
        results = append(results, result)
        successCount++
    }

    if len(iterationErrors) > 0 {
        if successCount > 0 {
            s.logger.Warn("GetNewData completed with partial data",
                slog.Int("success_count", successCount),
                slog.Int("error_count", len(iterationErrors)))
        } else {
            return nil, newTelemetryIterationError(iterationErrors[0], "GetNewData", len(iterationErrors), successCount > 0)
        }
    }

    return results, nil
}
```

### 3. Create Parameter Builder

```go
func (s *InfluxTelemetryStore) getNewDataParams(query models.NewDataQuery) influxdb3.QueryParameters {
    params := make(influxdb3.QueryParameters)
    params["device_ids"] = deviceIDsToStrings(query.DeviceIDs)
    // Note: No measurement_types parameter needed since we query single measurement

    if query.StartTime != nil {
        params["start_time"] = *query.StartTime
    } else {
        params["start_time"] = time.Now().Add(-24 * time.Hour)
    }

    if query.Limit != nil {
        params["limit"] = *query.Limit
    } else {
        params["limit"] = 1000
    }

    return params
}
```

### 4. Add Data Transformation (if needed)

In `models/query_results.go`:

```go
func (m *QueryResultMapper) ToNewDataResult(result *influxdb3.PointValues) models.NewDataResult {
    // Extract and transform data from InfluxDB result
    // Follow existing patterns in the file
}
```

### 5. Write Comprehensive Tests

```go
func TestInfluxTelemetryStore_GetNewData(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }

    t.Parallel() // Enable parallel execution

    container, testConfig := testutils.SetupInfluxDBContainer(t)
    defer func() {
        if err := container.Terminate(t.Context()); err != nil {
            t.Logf("Failed to terminate container: %v", err)
        }
    }()

    config := Config{
        URL:           testConfig.URL,
        Organization:  testConfig.Organization,
        Bucket:        testConfig.Bucket,
        Token:         testConfig.Token,
        WriteTimeout:  testConfig.WriteTimeout,
        QueryTimeout:  testConfig.QueryTimeout,
        RetryAttempts: 3,
        RetryDelay:    50 * time.Millisecond,
    }

    store, err := NewTelemetryStore(config)
    require.NoError(t, err)
    defer store.Close()

    ctx := t.Context()

    // Test the new method
    query := models.NewDataQuery{
        DeviceIDs: []models.DeviceID{"device1", "device2"},
        // ... other query parameters
    }

    results, err := store.GetNewData(ctx, query)
    require.NoError(t, err)
    
    // Verify results
    assert.NotNil(t, results)
    // ... additional assertions
}
```

## Critical Implementation Details

### Resource Management

```go
// Always implement proper cleanup
func (s *InfluxTelemetryStore) Close() error {
    if err := s.client.Close(); err != nil {
        return newTelemetryCloseError(err)
    }
    return nil
}

// Use defer for cleanup in tests
defer store.Close()
defer func() {
    if err := container.Terminate(t.Context()); err != nil {
        t.Logf("Failed to terminate container: %v", err)
    }
}()
```

### Context Handling

```go
// Always respect context cancellation
select {
case <-ctx.Done():
    return newTelemetryWriteError(ctx.Err(), len(points))
case <-time.After(delay):
    // Continue with retry
}

// Use t.Context() in tests, not context.Background()
ctx := t.Context()
```

### Type Safety

```go
// Use typed IDs and enums
type DeviceID string
type MeasurementType int

// Convert safely with helper functions
func deviceIDsToStrings(deviceIDs []models.DeviceID) []string {
    result := make([]string, len(deviceIDs))
    for i, id := range deviceIDs {
        result[i] = string(id)
    }
    return result
}
```

## Performance Optimization Guidelines

### Query Optimization

1. **Use indexed fields** in WHERE clauses
2. **Limit result sets** with reasonable LIMIT clauses
3. **Filter by time range** to reduce data scanned
4. **Use batch operations** for multiple writes
5. **Reuse connections** - don't create new clients per request

### Memory Management

1. **Stream large result sets** instead of loading all into memory
2. **Use appropriate buffer sizes** for channels and slices
3. **Clean up resources** with proper defer statements
4. **Avoid memory leaks** in long-running operations

### Retry Strategy

1. **Exponential backoff** with jitter to avoid thundering herd
2. **Maximum retry limits** to prevent infinite loops
3. **Circuit breaker pattern** for persistent failures
4. **Overflow protection** for very large retry counts

## Common Pitfalls and How to Avoid Them

### ❌ Common Mistakes

1. **🚨 CRITICAL: Using graceful test failures** - Never use `t.Skip()` to hide implementation bugs
2. **Separating queries from methods** - Keep queries immediately above methods
3. **Using generic error types** - Use specific error constructors
4. **Ignoring partial failures** - Handle iteration errors properly
5. **Hardcoding configuration** - Use environment variables and defaults
6. **Skipping resource cleanup** - Always defer cleanup operations
7. **Using context.Background() in tests** - Use t.Context()
8. **Arbitrary sleeps in tests** - Trust the health check mechanism
9. **Not handling container cleanup** - Always terminate test containers
10. **Forgetting t.Parallel()** - All integration tests should run in parallel

### ✅ Best Practices

1. **🎯 CRITICAL: Tests must fail fast** - Use `require.NoError()` to catch real bugs immediately
2. **Follow established patterns exactly** - Don't deviate without good reason
3. **Use structured logging** - Include relevant context in log messages
4. **Handle errors comprehensively** - Use appropriate error types
5. **Test both success and failure scenarios** - Include error case testing
6. **Use proper resource management** - Implement cleanup properly
7. **Respect context cancellation** - Check ctx.Done() in loops
8. **Use type-safe conversions** - Don't rely on interface{} casting
9. **Document new patterns** - Update this guide when adding new patterns

## Troubleshooting Guide

### Common Issues and Solutions

**Container startup failures:**
```bash
# Check Docker status
docker ps
docker logs <container_id>

# Verify image availability
docker pull influxdb:3.1-core
```

**"Invalid ticket" authentication errors:**
🚨 **CRITICAL**: These are NOT environment issues - they indicate query implementation bugs:
- ✅ **Fix the query syntax** - Use InfluxQL, not SQL
- ✅ **Use measurement_type in FROM clause** - Not bucket name
- ✅ **Use IN() operator** - Not ANY() which is unsupported in InfluxQL
- ✅ **Include measurement alias** - Add '%s' as measurement in SELECT
- ❌ **Don't skip tests** - Fix the implementation instead

**Query failures:**
- Verify InfluxQL syntax with InfluxDB v3 documentation
- Check parameter binding (use IN not ANY)
- Validate measurement names (not bucket names)
- Ensure proper measurement aliases in SELECT

**Test flakiness:**
- Remove arbitrary time.Sleep() calls
- Use proper health checks
- Ensure container cleanup

**Performance issues:**
- Check query complexity and indexing
- Verify proper LIMIT clauses
- Monitor memory usage in large result sets

### Debug Commands

```bash
# Run specific test with verbose output
go test -v -run TestInfluxTelemetryStore_Store_Data

# Run only unit tests (fast)
go test -short

# Check linting
just lint

# View container logs
docker logs $(docker ps -q --filter ancestor=influxdb:3.1-core)

# Test InfluxDB health
curl http://localhost:$(docker port <container> 8181/tcp | cut -d: -f2)/health
```

## Interface Compliance

The store must implement the complete `telemetry.TelemetryDataStore` interface:

```go
var _ telemetry.TelemetryDataStore = &InfluxTelemetryStore{}

type TelemetryDataStore interface {
    Store(ctx context.Context, data ...models.Telemetry) error
    StoreDeviceMetrics(ctx context.Context, data ...modelsV2.DeviceMetrics) error
    GetLatestDeviceMetricsBatch(ctx context.Context, deviceIDs []models.DeviceIdentifier) (map[models.DeviceIdentifier]modelsV2.DeviceMetrics, error)
    GetTimeSeriesTelemetry(ctx context.Context, query models.TimeSeriesTelemetryQuery) ([]models.Telemetry, error)
    StreamTelemetryUpdates(ctx context.Context, query models.StreamQuery) (<-chan models.TelemetryUpdate, error)
    GetCombinedMetrics(ctx context.Context, query models.CombinedMetricsQuery) (models.CombinedMetric, error)
    Ping(ctx context.Context) error
    Close() error
}
```

## Maintenance and Evolution

### When Adding New Features

1. **Follow all established patterns** - Don't introduce new patterns without discussion
2. **Add comprehensive tests** - Unit tests and integration tests
3. **Update documentation** - Add to this guide if new patterns emerge
4. **Maintain backward compatibility** - Don't break existing interfaces
5. **Consider performance impact** - Profile new code paths
6. **Update error handling** - Add new error types if needed

### Updating This Context.md File

**⚠️ IMPORTANT: Only update this context.md file when introducing genuinely new patterns or architectural changes.**

#### ✅ When to Update Context.md:
- **New architectural patterns** - If you introduce a fundamentally different way of doing something
- **New testing patterns** - If you create a new category of tests or testing utilities
- **New error handling patterns** - If you add new error types or handling strategies
- **New data transformation patterns** - If you add new conversion utilities or mapping strategies
- **Breaking changes** - If you modify existing interfaces or patterns
- **New performance optimizations** - If you introduce new performance-critical patterns
- **New configuration patterns** - If you add new configuration validation or handling

#### ❌ When NOT to Update Context.md:
- **Adding standard CRUD methods** - Follow existing patterns exactly, no documentation needed
- **Adding new query constants** - These follow the established query definition pattern
- **Adding new test cases** - Use existing test patterns, no new documentation needed
- **Bug fixes** - Unless they change fundamental patterns, don't update documentation
- **Minor refactoring** - Small improvements that don't change patterns
- **Adding new helper functions** - If they follow existing naming and structure conventions

#### How to Update Context.md:

1. **Identify the Pattern Change**:
   ```
   What new pattern am I introducing?
   Why is it different from existing patterns?
   Will other developers need to follow this pattern?
   ```

2. **Document the New Pattern**:
   - Add to the appropriate section (Implementation Patterns, Testing, etc.)
   - Include complete code examples
   - Explain why this pattern is preferred
   - Show both ✅ correct and ❌ incorrect usage

3. **Update Related Sections**:
   - Add to Code Review Checklist if it's a requirement
   - Add to Common Pitfalls if there are easy mistakes to make
   - Update Step-by-Step guides if the process changes

4. **Verify Consistency**:
   - Ensure new patterns don't contradict existing ones
   - Update examples throughout the document
   - Check that all code examples compile and work

#### Example of When to Update:

**✅ Good Reason to Update:**
```go
// NEW PATTERN: Streaming with backpressure control
func (s *InfluxTelemetryStore) StreamWithBackpressure(ctx context.Context, query models.StreamQuery) (<-chan models.TelemetryUpdate, error) {
    // This introduces a new pattern for handling backpressure
    // that other methods should follow
}
```

**❌ Bad Reason to Update:**
```go
// STANDARD PATTERN: Just another query method
func (s *InfluxTelemetryStore) GetDeviceHealth(ctx context.Context, query models.HealthQuery) ([]models.DeviceHealth, error) {
    // This follows existing query patterns exactly
    // No documentation update needed
}
```

### Documentation Maintenance Principles

1. **Keep It Current** - Remove outdated patterns when they're no longer used
2. **Keep It Concise** - Don't document every minor variation
3. **Keep It Practical** - Focus on patterns that developers will actually use
4. **Keep It Consistent** - Ensure all examples follow the same style
5. **Keep It Tested** - All code examples should be valid and tested

### Code Review Checklist

#### Implementation
- [ ] Query defined immediately above method
- [ ] Proper error handling with specific error types
- [ ] Structured logging with relevant context
- [ ] Resource cleanup with defer statements
- [ ] Context cancellation handling
- [ ] Parameter validation and defaults

#### Testing
- [ ] **CRITICAL: No graceful test failures** - Use `require.NoError()`, never `t.Skip()` for implementation issues
- [ ] Unit tests for configuration and validation
- [ ] Integration tests using testutils.SetupInfluxDBContainer()
- [ ] Short mode check: `if testing.Short() { t.Skip(...) }`
- [ ] Parallel execution: `t.Parallel()` for all integration tests
- [ ] Proper container cleanup with defer
- [ ] Use `t.Context()` instead of `context.Background()`
- [ ] Test both success and error scenarios
- [ ] Realistic test data and assertions
- [ ] Tests fail fast when there are real bugs

#### Quality
- [ ] Follows established naming conventions
- [ ] Proper documentation and comments
- [ ] No linting errors (`just lint` passes)
- [ ] No performance regressions
- [ ] Backward compatibility maintained

This comprehensive guide ensures consistency, maintainability, and reliability for all future development on the InfluxDB telemetry store package.
