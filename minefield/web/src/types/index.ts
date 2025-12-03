export interface ErrorDefinition {
  code: string
  name: string
  description: string
  category: string
  source?: string
  parameters: ParameterDefinition[]
}

export interface ParameterDefinition {
  name: string
  type: 'string' | 'number' | 'boolean' | 'array' | 'object'
  required: boolean
  description: string
  default?: any
}

export interface InjectedError {
  id: string
  error_code: string
  source: 'rig' | 'fan' | 'psu' | 'hashboard'
  message: string
  component_index?: number
  timestamp: number
  expired_at?: number
  ttl_seconds?: number
}

export interface TriggerErrorRequest {
  error_code: string
  source: 'rig' | 'fan' | 'psu' | 'hashboard'
  message?: string
  component_index?: number
  ttl_seconds?: number
}

export interface Status {
  status: string
  active_errors: number
  total_errors: number
}