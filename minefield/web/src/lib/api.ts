import type { ErrorDefinition, InjectedError, TriggerErrorRequest, Status } from '@/types'

const API_BASE = '/api'

async function fetchJSON(url: string, options?: RequestInit) {
  const response = await fetch(url, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  })

  if (!response.ok) {
    throw new Error(`HTTP error! status: ${response.status}`)
  }

  return response.json()
}

export const api = {
  // Get all error definitions
  getDefinitions: (): Promise<ErrorDefinition[]> =>
    fetchJSON(`${API_BASE}/errors/definitions`),

  // Get active errors
  getActiveErrors: (): Promise<InjectedError[]> =>
    fetchJSON(`${API_BASE}/errors/active`),

  // Get all errors (including expired)
  getAllErrors: (): Promise<InjectedError[]> =>
    fetchJSON(`${API_BASE}/errors/all`),

  // Trigger a new error
  triggerError: (request: TriggerErrorRequest): Promise<InjectedError> =>
    fetchJSON(`${API_BASE}/errors/trigger`, {
      method: 'POST',
      body: JSON.stringify(request),
    }),

  // Clear a specific error
  clearError: (id: string): Promise<void> =>
    fetch(`${API_BASE}/errors/${id}`, { method: 'DELETE' }).then(res => {
      if (!res.ok) throw new Error(`Failed to clear error: ${res.status}`)
    }),

  // Clear all errors
  clearAllErrors: (): Promise<void> =>
    fetch(`${API_BASE}/errors`, { method: 'DELETE' }).then(res => {
      if (!res.ok) throw new Error(`Failed to clear all errors: ${res.status}`)
    }),

  // Get proxy status
  getStatus: (): Promise<Status> =>
    fetchJSON(`${API_BASE}/status`),

  // Get error categories
  getCategories: (): Promise<string[]> =>
    fetchJSON(`${API_BASE}/errors/categories`),
}