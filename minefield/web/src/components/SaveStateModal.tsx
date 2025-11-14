import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Save, X } from 'lucide-react'
import { stateExists } from '@/lib/localStorage'
import type { InjectedError } from '@/types'

interface SaveStateModalProps {
  isOpen: boolean
  onClose: () => void
  onSave: (name: string, description?: string) => void
  currentErrors: InjectedError[]
}

export function SaveStateModal({ isOpen, onClose, onSave, currentErrors }: SaveStateModalProps) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [error, setError] = useState<string | null>(null)

  if (!isOpen) return null

  const handleSave = () => {
    setError(null)

    // Validation
    if (!name.trim()) {
      setError('Please enter a name for the saved state')
      return
    }

    if (stateExists(name)) {
      setError('A saved state with this name already exists')
      return
    }

    if (currentErrors.length === 0) {
      setError('No errors to save')
      return
    }

    // Save the state
    onSave(name.trim(), description.trim() || undefined)

    // Reset form and close
    setName('')
    setDescription('')
    setError(null)
    onClose()
  }

  const handleClose = () => {
    setName('')
    setDescription('')
    setError(null)
    onClose()
  }

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-black/50 z-40"
        onClick={handleClose}
      />

      {/* Modal */}
      <div className="fixed top-1/2 left-1/2 transform -translate-x-1/2 -translate-y-1/2 bg-white dark:bg-gray-800 rounded-lg shadow-xl z-50 w-full max-w-md p-6">
        <div className="flex justify-between items-center mb-4">
          <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">
            Save Error State
          </h2>
          <Button
            onClick={handleClose}
            variant="ghost"
            size="icon"
          >
            <X className="w-4 h-4" />
          </Button>
        </div>

        <div className="space-y-4">
          {/* Info */}
          <div className="text-sm text-gray-600 dark:text-gray-400">
            Saving {currentErrors.length} error{currentErrors.length !== 1 ? 's' : ''} to local storage
          </div>

          {/* Name Input */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Name *
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g., Fan failure test"
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100"
              autoFocus
            />
          </div>

          {/* Description Input */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Description (optional)
            </label>
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Optional notes about this error state"
              rows={3}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100"
            />
          </div>

          {/* Error Message */}
          {error && (
            <div className="p-3 bg-red-100 dark:bg-red-900 rounded-md">
              <p className="text-sm text-red-700 dark:text-red-200">
                {error}
              </p>
            </div>
          )}

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-2">
            <Button
              onClick={handleClose}
              variant="outline"
            >
              Cancel
            </Button>
            <Button
              onClick={handleSave}
              disabled={!name.trim() || currentErrors.length === 0}
            >
              <Save className="w-4 h-4 mr-2" />
              Save State
            </Button>
          </div>
        </div>
      </div>
    </>
  )
}