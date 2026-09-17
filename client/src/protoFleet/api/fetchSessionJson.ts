import { extractFetchError } from "./useFileUpload";
import { useFleetStore } from "@/protoFleet/store";

interface SessionJsonOptions {
  signal?: AbortSignal;
  logout: () => void;
  errorMessage: string;
}

class SessionExpiredError extends Error {
  constructor() {
    super("Session expired. Please log in again.");
  }
}

// Own the complete JSON read so endpoints cannot accidentally return stale
// bodies or log out a replacement login while handling an old response.
export async function fetchSessionJson<T>(
  url: string,
  { signal, logout, errorMessage }: SessionJsonOptions,
): Promise<T> {
  const { username, sessionGeneration, isAuthenticated } = useFleetStore.getState().auth;
  const assertCurrentRequest = () => {
    if (signal?.aborted) throw new DOMException("The operation was aborted.", "AbortError");
    const auth = useFleetStore.getState().auth;
    if (
      auth.username !== username ||
      auth.sessionGeneration !== sessionGeneration ||
      auth.isAuthenticated !== isAuthenticated
    ) {
      throw new Error("Your session changed. Refresh the page before trying again.");
    }
  };

  assertCurrentRequest();
  let data: T;
  try {
    const response = await fetch(url, { method: "GET", credentials: "include", signal });
    // Do not consume a body or handle authentication for an abandoned read.
    assertCurrentRequest();
    if (response.status === 401) throw new SessionExpiredError();
    if (!response.ok) {
      throw new Error(await extractFetchError(response, `${errorMessage}: ${response.status} ${response.statusText}`));
    }
    data = await response.json();
  } catch (error) {
    // Rejections can arrive after cancellation or a login change too. Keep
    // this check and logout synchronous, and never recheck after logout.
    assertCurrentRequest();
    if (error instanceof SessionExpiredError) logout();
    throw error;
  }
  assertCurrentRequest();
  return data;
}
