import type { InjectedError } from '@/types'

export interface SavedErrorState {
  name: string
  timestamp: number
  errors: InjectedError[]
  description?: string
}

const STORAGE_PREFIX = 'minefield'

// Get all saved error states
export function getSavedStates(): SavedErrorState[] {
  const states: SavedErrorState[] = []

  try {
    // Iterate through all localStorage keys
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i)
      if (key && key.startsWith(`${STORAGE_PREFIX}.`)) {
        const data = localStorage.getItem(key)
        if (data) {
          try {
            const state = JSON.parse(data) as SavedErrorState
            states.push(state)
          } catch (e) {
            console.error(`Failed to parse saved state: ${key}`, e)
          }
        }
      }
    }

    // Sort by timestamp (newest first)
    states.sort((a, b) => b.timestamp - a.timestamp)
  } catch (e) {
    console.error('Failed to get saved states', e)
  }

  return states
}

// Save an error state
export function saveErrorState(name: string, errors: InjectedError[], description?: string): boolean {
  try {
    const state: SavedErrorState = {
      name,
      timestamp: Date.now(),
      errors,
      description,
    }

    const key = `${STORAGE_PREFIX}.${name.replace(/[^a-zA-Z0-9_-]/g, '_')}`
    localStorage.setItem(key, JSON.stringify(state))
    return true
  } catch (e) {
    console.error('Failed to save error state', e)
    return false
  }
}

// Load an error state by name
export function loadErrorState(name: string): SavedErrorState | null {
  try {
    const key = `${STORAGE_PREFIX}.${name.replace(/[^a-zA-Z0-9_-]/g, '_')}`
    const data = localStorage.getItem(key)
    if (data) {
      return JSON.parse(data) as SavedErrorState
    }
  } catch (e) {
    console.error('Failed to load error state', e)
  }
  return null
}

// Delete a saved error state
export function deleteErrorState(name: string): boolean {
  try {
    const key = `${STORAGE_PREFIX}.${name.replace(/[^a-zA-Z0-9_-]/g, '_')}`
    localStorage.removeItem(key)
    return true
  } catch (e) {
    console.error('Failed to delete error state', e)
    return false
  }
}

// Check if a state name already exists
export function stateExists(name: string): boolean {
  const key = `${STORAGE_PREFIX}.${name.replace(/[^a-zA-Z0-9_-]/g, '_')}`
  return localStorage.getItem(key) !== null
}

// Get the count of saved states
export function getSavedStatesCount(): number {
  let count = 0
  for (let i = 0; i < localStorage.length; i++) {
    const key = localStorage.key(i)
    if (key && key.startsWith(`${STORAGE_PREFIX}.`)) {
      count++
    }
  }
  return count
}

// Clear all saved states
export function clearAllSavedStates(): boolean {
  try {
    const keysToRemove: string[] = []
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i)
      if (key && key.startsWith(`${STORAGE_PREFIX}.`)) {
        keysToRemove.push(key)
      }
    }

    keysToRemove.forEach(key => localStorage.removeItem(key))
    return true
  } catch (e) {
    console.error('Failed to clear all saved states', e)
    return false
  }
}

// Format timestamp for display
export function formatTimestamp(timestamp: number): string {
  const date = new Date(timestamp)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  const diffHours = Math.floor(diffMs / 3600000)
  const diffDays = Math.floor(diffMs / 86400000)

  if (diffMins < 1) return 'Just now'
  if (diffMins < 60) return `${diffMins} minute${diffMins === 1 ? '' : 's'} ago`
  if (diffHours < 24) return `${diffHours} hour${diffHours === 1 ? '' : 's'} ago`
  if (diffDays < 7) return `${diffDays} day${diffDays === 1 ? '' : 's'} ago`

  return date.toLocaleDateString()
}