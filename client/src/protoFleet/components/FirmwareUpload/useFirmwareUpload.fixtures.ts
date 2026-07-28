import { vi } from "vitest";

/**
 * Builds a useFirmwareUpload return value for tests that mock the hook. Keeps
 * the fake in one place so a new field on the hook does not have to be added to
 * every `mockReturnValue` call across the dialogs that consume it.
 */
export const uploadHookState = (overrides: Record<string, unknown> = {}) => ({
  state: "idle",
  file: null,
  firmwareFileId: null,
  uploadProgress: 0,
  errorMessage: null,
  serverConfig: null,
  processFile: vi.fn(),
  reset: vi.fn(),
  retry: vi.fn(),
  ...overrides,
});
