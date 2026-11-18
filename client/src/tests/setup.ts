import "@testing-library/jest-dom/vitest";
import { afterAll, expect, vi } from "vitest";
import * as matchers from "@testing-library/jest-dom/matchers";

expect.extend(matchers);

// Recharts batches chart store updates with requestAnimationFrame and a 100 ms fallback timer. If a file ends
// right after a chart update, the timer can fire after jsdom teardown and call the removed cancelAnimationFrame.
// Let pending frames run while jsdom still exists. The cap covers a requestAnimationFrame stub that never calls back.
afterAll(async () => {
  if (typeof window === "undefined" || typeof requestAnimationFrame !== "function") return;
  vi.useRealTimers();
  await new Promise<void>((resolve) => {
    const cap = setTimeout(resolve, 150);
    requestAnimationFrame(() =>
      requestAnimationFrame(() => {
        clearTimeout(cap);
        resolve();
      }),
    );
  });
});

// Datadog RUM SDK — mocked globally so tests never load the real SDK or hit the
// network. Provider tests assert against these spies.
vi.mock("@datadog/browser-rum", () => ({
  datadogRum: {
    init: vi.fn(),
    addError: vi.fn(),
  },
}));

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
