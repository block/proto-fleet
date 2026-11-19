const openLockDatabase = (name: string) =>
  new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open(name, 1);
    request.onupgradeneeded = () => request.result.createObjectStore("lock");
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });

type PoolWrite = (signal: AbortSignal) => Promise<void>;

export async function withPoolWriteLock(controllerId: string, signal: AbortSignal, write: PoolWrite) {
  const name = `proto-os:pool-write:${encodeURIComponent(controllerId)}`;
  if (navigator.locks) {
    return navigator.locks.request(name, { signal }, () => write(signal));
  }

  // Direct LAN HTTP does not expose Web Locks. A readwrite transaction on
  // this controller's store also serializes tabs and releases on tab exit.
  const database = await openLockDatabase(name);
  try {
    signal.throwIfAborted();
    await new Promise<void>((resolve, reject) => {
      const transaction = database.transaction("lock", "readwrite");
      const store = transaction.objectStore("lock");
      const guard = new AbortController();
      const writeSignal = AbortSignal.any([signal, guard.signal]);
      let result: Promise<void> | undefined;
      let finished = false;
      const cancelWhileWaiting = () => {
        if (!result) transaction.abort();
      };
      const keepAlive = () => {
        // IndexedDB commits once its last request ends. Keep one empty read
        // pending while waiting for the controller, without storing payloads.
        store.get(0).onsuccess = () => {
          if (finished) return;
          if (!result) {
            result = Promise.resolve().then(() => {
              writeSignal.throwIfAborted();
              return write(writeSignal);
            });
            const finish = () => {
              finished = true;
            };
            void result.then(finish, finish);
          }
          keepAlive();
        };
      };
      transaction.oncomplete = () => {
        signal.removeEventListener("abort", cancelWhileWaiting);
        void result?.then(resolve, reject);
      };
      transaction.onabort = () => {
        signal.removeEventListener("abort", cancelWhileWaiting);
        const error = signal.reason ?? transaction.error ?? new Error("The pool write lock was lost.");
        guard.abort(error);
        reject(error);
      };
      signal.addEventListener("abort", cancelWhileWaiting, { once: true });
      keepAlive();
    });
  } finally {
    database.close();
  }
}
