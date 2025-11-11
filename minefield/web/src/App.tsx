import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ErrorDefinition, TriggerErrorRequest } from '@/types'
import { Button } from '@/components/ui/button'
import { AlertCircle, CheckCircle, Trash2, RefreshCw, Send } from 'lucide-react'

function App() {
  const queryClient = useQueryClient()
  const [selectedError, setSelectedError] = useState<ErrorDefinition | null>(null)
  const [formData, setFormData] = useState<Record<string, any>>({})
  const [ttl, setTtl] = useState<number>(0)
  const [selectedCategory, setSelectedCategory] = useState<string>('All')

  // Queries
  const { data: definitions = [] } = useQuery({
    queryKey: ['definitions'],
    queryFn: api.getDefinitions,
  })

  const { data: activeErrors = [], isLoading: loadingErrors } = useQuery({
    queryKey: ['activeErrors'],
    queryFn: api.getActiveErrors,
  })

  const { data: status } = useQuery({
    queryKey: ['status'],
    queryFn: api.getStatus,
  })

  const { data: categories = [] } = useQuery({
    queryKey: ['categories'],
    queryFn: api.getCategories,
  })

  // Mutations
  const triggerMutation = useMutation({
    mutationFn: (request: TriggerErrorRequest) => api.triggerError(request),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['activeErrors'] })
      queryClient.invalidateQueries({ queryKey: ['status'] })
      setFormData({})
      setSelectedError(null)
    },
  })

  const clearMutation = useMutation({
    mutationFn: (id: string) => api.clearError(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['activeErrors'] })
      queryClient.invalidateQueries({ queryKey: ['status'] })
    },
  })

  const clearAllMutation = useMutation({
    mutationFn: api.clearAllErrors,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['activeErrors'] })
      queryClient.invalidateQueries({ queryKey: ['status'] })
    },
  })

  // Filter definitions by category
  const filteredDefinitions = selectedCategory === 'All'
    ? definitions
    : definitions.filter(d => d.category === selectedCategory)

  const handleTriggerError = () => {
    if (!selectedError) return

    const details: Record<string, any> = {}
    selectedError.parameters.forEach(param => {
      if (formData[param.name] !== undefined) {
        // Convert values to appropriate types
        if (param.type === 'number') {
          details[param.name] = Number(formData[param.name])
        } else if (param.type === 'array') {
          // Handle array input (comma-separated values)
          const value = formData[param.name]
          if (typeof value === 'string') {
            details[param.name] = value.split(',').map((v: string) => v.trim())
          } else {
            details[param.name] = value
          }
        } else {
          details[param.name] = formData[param.name]
        }
      } else if (param.default !== undefined) {
        details[param.name] = param.default
      }
    })

    const request: TriggerErrorRequest = {
      error_code: selectedError.code,
      error_level: selectedError.default_level,
      message: selectedError.description,
      details,
    }

    if (ttl > 0) {
      request.ttl_seconds = ttl
    }

    triggerMutation.mutate(request)
  }

  return (
    <div className="min-h-screen bg-gray-50 dark:bg-gray-900">
      <div className="container mx-auto py-8 px-4">
        <header className="mb-8">
          <h1 className="text-4xl font-bold text-gray-900 dark:text-gray-100 mb-2">
            Minefield Error Injection
          </h1>
          <div className="flex items-center gap-4 text-sm text-gray-600 dark:text-gray-400">
            <span className="flex items-center gap-1">
              {status?.status === 'running' ? (
                <CheckCircle className="w-4 h-4 text-green-500" />
              ) : (
                <AlertCircle className="w-4 h-4 text-yellow-500" />
              )}
              Proxy Status: {status?.status || 'Unknown'}
            </span>
            <span>Active Errors: {status?.active_errors || 0}</span>
            <span>Total Errors: {status?.total_errors || 0}</span>
          </div>
        </header>

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
          {/* Error Trigger Panel */}
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-md p-6">
            <h2 className="text-2xl font-semibold mb-4 text-gray-900 dark:text-gray-100">
              Trigger Error
            </h2>

            {/* Category Filter */}
            <div className="mb-4">
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                Category
              </label>
              <select
                value={selectedCategory}
                onChange={(e) => setSelectedCategory(e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100"
              >
                <option value="All">All Categories</option>
                {categories.map(cat => (
                  <option key={cat} value={cat}>{cat}</option>
                ))}
              </select>
            </div>

            {/* Error Type Selection */}
            <div className="mb-4">
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                Error Type
              </label>
              <select
                value={selectedError?.code || ''}
                onChange={(e) => {
                  const error = definitions.find(d => d.code === e.target.value)
                  setSelectedError(error || null)
                  setFormData({})
                }}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100"
              >
                <option value="">Select an error type</option>
                {filteredDefinitions.map(def => (
                  <option key={def.code} value={def.code}>
                    {def.name} ({def.code})
                  </option>
                ))}
              </select>
            </div>

            {/* Error Description */}
            {selectedError && (
              <div className="mb-4 p-3 bg-gray-100 dark:bg-gray-700 rounded-md">
                <p className="text-sm text-gray-700 dark:text-gray-300">
                  {selectedError.description}
                </p>
                <p className="text-xs mt-1 text-gray-500 dark:text-gray-400">
                  Level: <span className={selectedError.default_level === 'Error' ? 'text-red-500' : 'text-yellow-500'}>
                    {selectedError.default_level}
                  </span>
                </p>
              </div>
            )}

            {/* Parameter Fields */}
            {selectedError && selectedError.parameters.length > 0 && (
              <div className="mb-4">
                <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                  Parameters
                </h3>
                <div className="space-y-3">
                  {selectedError.parameters.map(param => (
                    <div key={param.name}>
                      <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
                        {param.name} {param.required && <span className="text-red-500">*</span>}
                        <span className="ml-2 text-gray-500">({param.type})</span>
                      </label>
                      <input
                        type={param.type === 'number' ? 'number' : 'text'}
                        value={formData[param.name] || ''}
                        onChange={(e) => setFormData({
                          ...formData,
                          [param.name]: e.target.value
                        })}
                        placeholder={param.description}
                        className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100"
                      />
                      {param.type === 'array' && (
                        <p className="text-xs text-gray-500 mt-1">
                          Comma-separated values
                        </p>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* TTL Field */}
            <div className="mb-6">
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                TTL (seconds, 0 = no expiry)
              </label>
              <input
                type="number"
                min="0"
                value={ttl}
                onChange={(e) => setTtl(Number(e.target.value))}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100"
              />
            </div>

            {/* Trigger Button */}
            <Button
              onClick={handleTriggerError}
              disabled={!selectedError || triggerMutation.isPending}
              className="w-full"
            >
              <Send className="w-4 h-4 mr-2" />
              {triggerMutation.isPending ? 'Triggering...' : 'Trigger Error'}
            </Button>

            {triggerMutation.isError && (
              <div className="mt-4 p-3 bg-red-100 dark:bg-red-900 rounded-md">
                <p className="text-sm text-red-700 dark:text-red-200">
                  Failed to trigger error
                </p>
              </div>
            )}

            {triggerMutation.isSuccess && (
              <div className="mt-4 p-3 bg-green-100 dark:bg-green-900 rounded-md">
                <p className="text-sm text-green-700 dark:text-green-200">
                  Error triggered successfully!
                </p>
              </div>
            )}
          </div>

          {/* Active Errors Panel */}
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-md p-6">
            <div className="flex justify-between items-center mb-4">
              <h2 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">
                Active Errors
              </h2>
              <div className="flex gap-2">
                <Button
                  onClick={() => queryClient.invalidateQueries({ queryKey: ['activeErrors'] })}
                  variant="outline"
                  size="icon"
                  disabled={loadingErrors}
                >
                  <RefreshCw className={`w-4 h-4 ${loadingErrors ? 'animate-spin' : ''}`} />
                </Button>
                <Button
                  onClick={() => clearAllMutation.mutate()}
                  variant="destructive"
                  size="sm"
                  disabled={activeErrors.length === 0 || clearAllMutation.isPending}
                >
                  Clear All
                </Button>
              </div>
            </div>

            {activeErrors.length === 0 ? (
              <div className="text-center py-12 text-gray-500 dark:text-gray-400">
                No active errors
              </div>
            ) : (
              <div className="space-y-3">
                {activeErrors.map(error => (
                  <div
                    key={error.id}
                    className="border border-gray-200 dark:border-gray-700 rounded-lg p-4"
                  >
                    <div className="flex justify-between items-start">
                      <div className="flex-1">
                        <div className="flex items-center gap-2">
                          <h3 className="font-semibold text-gray-900 dark:text-gray-100">
                            {error.error_code}
                          </h3>
                          <span
                            className={`px-2 py-1 text-xs rounded-md ${
                              error.error_level === 'Error'
                                ? 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-200'
                                : 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-200'
                            }`}
                          >
                            {error.error_level}
                          </span>
                        </div>
                        <p className="text-sm text-gray-600 dark:text-gray-400 mt-1">
                          {error.message}
                        </p>
                        {error.details && Object.keys(error.details).length > 0 && (
                          <div className="mt-2">
                            <details className="cursor-pointer">
                              <summary className="text-xs text-gray-500 dark:text-gray-400">
                                Details
                              </summary>
                              <pre className="mt-2 text-xs bg-gray-100 dark:bg-gray-700 p-2 rounded overflow-x-auto">
                                {JSON.stringify(error.details, null, 2)}
                              </pre>
                            </details>
                          </div>
                        )}
                        <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">
                          Created: {new Date(error.inserted_at * 1000).toLocaleString()}
                          {error.ttl_seconds && ` • TTL: ${error.ttl_seconds}s`}
                        </p>
                      </div>
                      <Button
                        onClick={() => clearMutation.mutate(error.id)}
                        variant="ghost"
                        size="icon"
                        disabled={clearMutation.isPending}
                      >
                        <Trash2 className="w-4 h-4" />
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

export default App