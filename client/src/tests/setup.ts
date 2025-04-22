import "@testing-library/jest-dom/vitest";
import { expect, vi } from "vitest";
import * as matchers from "@testing-library/jest-dom/matchers";

expect.extend(matchers);

// eslint-disable-next-line no-undef
global.ResizeObserver = vi.fn().mockImplementation(() => ({
  observe: vi.fn(),
  unobserve: vi.fn(),
  disconnect: vi.fn(),
}));
