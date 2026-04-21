import "@testing-library/jest-dom/vitest";
import { expect, vi } from "vitest";
import * as matchers from "@testing-library/jest-dom/matchers";

expect.extend(matchers);

// eslint-disable-next-line no-undef
global.ResizeObserver = class ResizeObserver {
  observe = vi.fn();
  unobserve = vi.fn();
  disconnect = vi.fn();
};

// eslint-disable-next-line no-undef
global.MutationObserver = class MutationObserver {
  observe = vi.fn();
  disconnect = vi.fn();
  takeRecords = vi.fn();
};

// eslint-disable-next-line no-undef
global.IntersectionObserver = class IntersectionObserver {
  root = null;
  rootMargin = "";
  scrollMargin = "";
  thresholds = [];
  disconnect = vi.fn();
  observe = vi.fn();
  takeRecords = vi.fn();
  unobserve = vi.fn();
};
