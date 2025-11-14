import { Button } from '@/components/ui/button'
import { Download, Trash2, X, FolderOpen } from 'lucide-react'
import { formatTimestamp, type SavedErrorState } from '@/lib/localStorage'

interface RecallStateModalProps {
  isOpen: boolean
  onClose: () => void
  savedStates: SavedErrorState[]
  onLoad: (state: SavedErrorState) => void
  onDelete: (name: string) => void
}

export function RecallStateModal({
  isOpen,
  onClose,
  savedStates,
  onLoad,
  onDelete,
}: RecallStateModalProps) {
  if (!isOpen) return null

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-black/50 z-40"
        onClick={onClose}
      />

      {/* Modal */}
      <div className="fixed top-1/2 left-1/2 transform -translate-x-1/2 -translate-y-1/2 bg-white dark:bg-gray-800 rounded-lg shadow-xl z-50 w-full max-w-2xl max-h-[80vh] overflow-hidden flex flex-col">
        <div className="flex justify-between items-center p-6 border-b border-gray-200 dark:border-gray-700">
          <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">
            Recall Error State
          </h2>
          <Button
            onClick={onClose}
            variant="ghost"
            size="icon"
          >
            <X className="w-4 h-4" />
          </Button>
        </div>

        <div className="flex-1 overflow-y-auto p-6">
          {savedStates.length === 0 ? (
            <div className="text-center py-12">
              <FolderOpen className="w-16 h-16 mx-auto text-gray-400 mb-4" />
              <p className="text-gray-500 dark:text-gray-400">
                No saved error states
              </p>
              <p className="text-sm text-gray-400 dark:text-gray-500 mt-2">
                Save your current error configuration to recall it later
              </p>
            </div>
          ) : (
            <div className="space-y-3">
              {savedStates.map((state) => (
                <div
                  key={state.name}
                  className="border border-gray-200 dark:border-gray-700 rounded-lg p-4 hover:bg-gray-50 dark:hover:bg-gray-700/50 transition-colors"
                >
                  <div className="flex justify-between items-start">
                    <div className="flex-1">
                      <h3 className="font-semibold text-gray-900 dark:text-gray-100">
                        {state.name}
                      </h3>
                      {state.description && (
                        <p className="text-sm text-gray-600 dark:text-gray-400 mt-1">
                          {state.description}
                        </p>
                      )}
                      <div className="flex items-center gap-4 mt-2 text-xs text-gray-500 dark:text-gray-400">
                        <span>{state.errors.length} error{state.errors.length !== 1 ? 's' : ''}</span>
                        <span>•</span>
                        <span>{formatTimestamp(state.timestamp)}</span>
                      </div>

                      {/* Show error types */}
                      <div className="mt-2 flex flex-wrap gap-1">
                        {state.errors.slice(0, 5).map((error, idx) => (
                          <span
                            key={idx}
                            className="text-xs px-2 py-1 bg-gray-100 dark:bg-gray-700 rounded-md text-gray-600 dark:text-gray-300"
                          >
                            {error.error_code}
                          </span>
                        ))}
                        {state.errors.length > 5 && (
                          <span className="text-xs px-2 py-1 text-gray-500 dark:text-gray-400">
                            +{state.errors.length - 5} more
                          </span>
                        )}
                      </div>
                    </div>

                    <div className="flex gap-2 ml-4">
                      <Button
                        onClick={() => {
                          onLoad(state)
                          onClose()
                        }}
                        size="sm"
                      >
                        <Download className="w-4 h-4 mr-2" />
                        Load
                      </Button>
                      <Button
                        onClick={() => onDelete(state.name)}
                        variant="ghost"
                        size="icon"
                      >
                        <Trash2 className="w-4 h-4 text-red-500" />
                      </Button>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </>
  )
}