/**
 * Global stream cleanup manager to handle page unload
 * Ensures all streaming connections are aborted when the page refreshes/closes
 */
class StreamCleanupManager {
  private controllers: Set<AbortController> = new Set();

  register(controller: AbortController) {
    this.controllers.add(controller);
  }

  unregister(controller: AbortController) {
    this.controllers.delete(controller);
  }

  abortAll() {
    this.controllers.forEach((controller) => {
      try {
        controller.abort();
      } catch {
        // Ignore errors during cleanup
      }
    });
    this.controllers.clear();
  }
}

export const streamCleanupManager = new StreamCleanupManager();

// Register global cleanup handlers
if (typeof window !== "undefined") {
  // Clean up on page unload/refresh
  window.addEventListener("beforeunload", () => {
    streamCleanupManager.abortAll();
  });

  // Also clean up on visibility change (tab switching)
  document.addEventListener("visibilitychange", () => {
    if (document.hidden) {
      streamCleanupManager.abortAll();
    }
  });
}
