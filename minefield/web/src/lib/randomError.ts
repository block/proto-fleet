import type { ErrorDefinition, TriggerErrorRequest } from '@/types'

// Realistic parameter ranges for mining hardware errors
const PARAMETER_RANGES = {
  // ASIC parameters
  asic_index: { min: 0, max: 255 },
  temperature: { min: 70, max: 110 }, // Celsius
  voltage: { min: 8.5, max: 10.5 }, // Volts
  ecc_error_count: { min: 1, max: 100 },
  enumeration_count: { min: 0, max: 255 },
  hash_count: { min: 0, max: 1000000 },

  // Hashboard parameters
  hb_slot: { min: 0, max: 2 }, // Usually 0-2 for 3 hashboards
  hb_sn: ['HB00001', 'HB00002', 'HB00003', 'HB00004', 'HB00005'], // Sample serial numbers
  current: { min: 20, max: 80 }, // Amps

  // Fan parameters
  fan_bay_index: { min: 0, max: 3 },
  fan_id: { min: 0, max: 5 },
  fan_pwm_target_pct: { min: 20, max: 100 }, // Percentage
  fan_rpm_tach: { min: 0, max: 6000 }, // RPM
  failed_fans: [[0], [1], [0, 1], [2], [0, 2], [1, 2]], // Common failure patterns

  // PSU parameters
  psu_bay_index: { min: 0, max: 1 }, // Usually 0-1 for dual PSU
  psu_index: { min: 0, max: 1 },
  psu_sn: ['PSU00001', 'PSU00002', 'PSU00003', 'PSU00004'], // Sample serial numbers

  // System parameters
  hashboard_types: [['S19', 'S19'], ['S19', 'S19Pro'], ['S19Pro', 'S19Pro'], ['T19', 'T19']], // Type combinations
  insufficient_cooling: { min: 5, max: 30 }, // Temperature delta

  // Pool parameters
  pool_url: ['stratum+tcp://pool.example.com:3333', 'stratum+tcp://us.pool.com:1234', 'stratum+tcp://eu.pool.com:5678'],
  pool_user: ['miner.worker1', 'miner.worker2', 'btc.rig1', 'farm.asic1'],
}

// Generate a random integer between min and max (inclusive)
function randomInt(min: number, max: number): number {
  return Math.floor(Math.random() * (max - min + 1)) + min
}

// Generate a random float between min and max
function randomFloat(min: number, max: number, decimals: number = 2): number {
  const value = Math.random() * (max - min) + min
  return parseFloat(value.toFixed(decimals))
}

// Pick a random element from an array
function randomChoice<T>(array: T[]): T {
  return array[Math.floor(Math.random() * array.length)]
}

// Generate random parameter value based on parameter name and type
function generateParameterValue(paramName: string, paramType: string): unknown {
  // Check for specific parameter ranges
  if (paramName in PARAMETER_RANGES) {
    const range = PARAMETER_RANGES[paramName as keyof typeof PARAMETER_RANGES]

    if (Array.isArray(range)) {
      // It's an array of choices - directly index instead of using randomChoice to avoid type issues
      return range[Math.floor(Math.random() * range.length)]
    } else if (typeof range === 'object' && 'min' in range && 'max' in range) {
      // It's a numeric range
      if (paramType === 'number') {
        // Use float for voltage/current, int for others
        if (paramName.includes('voltage') || paramName.includes('current')) {
          return randomFloat(range.min, range.max)
        }
        return randomInt(range.min, range.max)
      }
    }
  }

  // Fallback for unknown parameters
  switch (paramType) {
    case 'number':
      return randomInt(0, 100)
    case 'string':
      return `value_${randomInt(1, 1000)}`
    case 'boolean':
      return Math.random() > 0.5
    case 'array':
      // Generate a small array of random values
      const arrayLength = randomInt(1, 3)
      const items = []
      for (let i = 0; i < arrayLength; i++) {
        items.push(randomInt(0, 10))
      }
      return items
    default:
      return null
  }
}

// Generate random parameters for an error definition
export function generateRandomParameters(errorDef: ErrorDefinition): Record<string, unknown> {
  const params: Record<string, unknown> = {}

  errorDef.parameters.forEach(param => {
    // Always include required parameters
    if (param.required || Math.random() > 0.3) { // 70% chance to include optional params
      params[param.name] = generateParameterValue(param.name, param.type)
    }
  })

  return params
}

// Generate a random TTL between 30 and 300 seconds
export function generateRandomTTL(): number {
  const ttlOptions = [0, 30, 60, 120, 180, 300] // Include 0 for permanent errors
  return randomChoice(ttlOptions)
}

// Create a trigger request for a random error
// Helper to determine source from error category
function getSourceFromCategory(category: string): 'rig' | 'fan' | 'psu' | 'hashboard' {
  const categoryLower = category.toLowerCase()
  if (categoryLower === 'cooling') return 'fan'
  if (categoryLower === 'psu') return 'psu'
  if (categoryLower === 'hashboard' || categoryLower === 'asic') return 'hashboard'
  if (categoryLower === 'pool' || categoryLower === 'system') return 'rig'
  return 'rig' // Default fallback
}

export function createRandomErrorRequest(errorDef: ErrorDefinition): TriggerErrorRequest {
  const parameters = generateRandomParameters(errorDef)
  const ttl = generateRandomTTL()

  // Extract component_index if present in parameters
  const componentIndex = parameters.component_index !== undefined
    ? Number(parameters.component_index)
    : parameters.fan_bay_index !== undefined
    ? Number(parameters.fan_bay_index)
    : parameters.psu_index !== undefined
    ? Number(parameters.psu_index)
    : parameters.hb_slot !== undefined
    ? Number(parameters.hb_slot)
    : undefined

  const request: TriggerErrorRequest = {
    error_code: errorDef.code,
    source: errorDef.source as 'rig' | 'fan' | 'psu' | 'hashboard' || getSourceFromCategory(errorDef.category),
    message: errorDef.description,
    component_index: componentIndex,
  }

  if (ttl > 0) {
    request.ttl_seconds = ttl
  }

  return request
}

// Pick N random errors from the available definitions
export function selectRandomErrors(
  definitions: ErrorDefinition[],
  count: number,
  category?: string
): ErrorDefinition[] {
  // Filter by category if specified
  let available = category && category !== 'All'
    ? definitions.filter(d => d.category === category)
    : definitions

  // Shuffle and pick the first N
  const shuffled = [...available].sort(() => Math.random() - 0.5)
  return shuffled.slice(0, Math.min(count, shuffled.length))
}

// Roll a dice and return the result (1-6)
export function rollDice(): Promise<number> {
  return new Promise((resolve) => {
    // Simulate dice rolling animation delay
    setTimeout(() => {
      resolve(randomInt(1, 6))
    }, 1000) // 1 second animation
  })
}

// Generate requests for all error types with random parameters
export function generateAllErrorRequests(definitions: ErrorDefinition[]): TriggerErrorRequest[] {
  return definitions.map(def => createRandomErrorRequest(def))
}