export interface ErrorDefinition {
  code: string
  name: string
  description: string
  category: string
  default_level: 'Error' | 'Warning'
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
  error_level: 'Error' | 'Warning'
  message: string
  details: Record<string, any>
  component_index?: number
  hashboard_index?: number
  asic_index?: number
  inserted_at: number
  expired_at?: number
  ttl_seconds?: number
}

export interface TriggerErrorRequest {
  error_code: string
  error_level?: 'Error' | 'Warning'
  message?: string
  details?: Record<string, any>
  ttl_seconds?: number
}

export interface Status {
  status: string
  active_errors: number
  total_errors: number
}