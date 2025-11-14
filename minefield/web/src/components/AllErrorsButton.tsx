import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { AlertTriangle, Loader2 } from 'lucide-react'
import type { ErrorDefinition } from '@/types'

interface AllErrorsButtonProps {
  definitions: ErrorDefinition[]
  onTrigger: (definitions: ErrorDefinition[], onProgress?: (current: number) => void) => Promise<void>
  disabled?: boolean
}

export function AllErrorsButton({ definitions, onTrigger, disabled }: AllErrorsButtonProps) {
  const [showModal, setShowModal] = useState(false)
  const [isTriggering, setIsTriggering] = useState(false)
  const [progress, setProgress] = useState(0)

  const handleConfirm = async () => {
    setIsTriggering(true)
    setProgress(0)

    try {
      await onTrigger(definitions, setProgress)
      setShowModal(false)
    } catch (error) {
      console.error('Failed to trigger all errors', error)
    } finally {
      setIsTriggering(false)
      setProgress(0)
    }
  }

  return (
    <>
      <Button
        onClick={() => setShowModal(true)}
        variant="outline"
        size="sm"
        disabled={disabled || definitions.length === 0}
        className="text-yellow-600 dark:text-yellow-400 hover:text-yellow-700 dark:hover:text-yellow-300"
        title="Trigger all available error types"
      >
        <AlertTriangle className="w-4 h-4 mr-2" />
        All Errors
      </Button>

      {/* Confirmation Modal */}
      {showModal && (
        <>
          {/* Backdrop */}
          <div
            className="fixed inset-0 bg-black/50 z-40"
            onClick={() => !isTriggering && setShowModal(false)}
          />

          {/* Modal */}
          <div className="fixed top-1/2 left-1/2 transform -translate-x-1/2 -translate-y-1/2 bg-white dark:bg-gray-800 rounded-lg shadow-xl z-50 w-full max-w-md p-6">
            <div className="flex items-center mb-4">
              <AlertTriangle className="w-8 h-8 text-yellow-500 mr-3" />
              <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">
                Trigger All Errors?
              </h2>
            </div>

            {!isTriggering ? (
              <>
                <div className="mb-6">
                  <p className="text-gray-600 dark:text-gray-400 mb-4">
                    This will trigger <strong>{definitions.length} different error types</strong> with random realistic parameters.
                  </p>
                  <div className="bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded-md p-3">
                    <p className="text-sm text-yellow-800 dark:text-yellow-200">
                      <strong>Warning:</strong> This will clear all existing errors before triggering the new ones.
                    </p>
                  </div>

                  {/* Show error categories summary */}
                  <div className="mt-4">
                    <p className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                      Error types by category:
                    </p>
                    <div className="space-y-1">
                      {Object.entries(
                        definitions.reduce((acc, def) => {
                          acc[def.category] = (acc[def.category] || 0) + 1
                          return acc
                        }, {} as Record<string, number>)
                      ).map(([category, count]) => (
                        <div key={category} className="flex justify-between text-sm text-gray-600 dark:text-gray-400">
                          <span>{category}:</span>
                          <span>{count} errors</span>
                        </div>
                      ))}
                    </div>
                  </div>
                </div>

                <div className="flex justify-end gap-2">
                  <Button
                    onClick={() => setShowModal(false)}
                    variant="outline"
                  >
                    Cancel
                  </Button>
                  <Button
                    onClick={handleConfirm}
                    className="bg-yellow-600 hover:bg-yellow-700 text-white"
                  >
                    <AlertTriangle className="w-4 h-4 mr-2" />
                    Trigger All {definitions.length} Errors
                  </Button>
                </div>
              </>
            ) : (
              <>
                <div className="mb-6">
                  <p className="text-gray-600 dark:text-gray-400 mb-4">
                    Triggering errors, please wait...
                  </p>

                  {/* Progress Bar */}
                  <div className="mb-2">
                    <div className="flex justify-between text-sm text-gray-600 dark:text-gray-400 mb-1">
                      <span>Progress</span>
                      <span>{Math.round((progress / definitions.length) * 100)}%</span>
                    </div>
                    <div className="w-full bg-gray-200 dark:bg-gray-700 rounded-full h-2">
                      <div
                        className="bg-yellow-600 h-2 rounded-full transition-all duration-300"
                        style={{ width: `${(progress / definitions.length) * 100}%` }}
                      />
                    </div>
                  </div>

                  <div className="flex items-center justify-center text-sm text-gray-600 dark:text-gray-400 mt-4">
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Triggering {progress} of {definitions.length} errors...
                  </div>
                </div>
              </>
            )}
          </div>
        </>
      )}
    </>
  )
}