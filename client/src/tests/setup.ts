import "@testing-library/jest-dom/vitest";
import { afterAll, vi } from "vitest";

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

// A Motion entrance animation renders opacity zero first, which toBeVisible() rejects, and shows the content on
// a later animation frame. jsdom runs frames on a wall-clock timer, so a query could find the content before
// that frame, most often on a busy CI worker. In tests, Motion applies each animation target at its first frame,
// and that frame runs as a microtask, before a timer can run a query. Motion binds its frame scheduler when it
// loads, so this factory loads it with the microtask scheduler in place, then restores the jsdom one for other
// callers. The module is real Motion; the factory changes only its configuration.
vi.mock("motion/react", async (importOriginal) => {
  const jsdomRequestAnimationFrame = globalThis.requestAnimationFrame;
  const { setTimeout: realSetTimeout } = globalThis;
  // A frame that always requests another frame must not hold the event loop.
  const maxFramesPerTask = 50;
  let framesInThisTask = 0;
  globalThis.requestAnimationFrame = (callback) => {
    const runFrame = () => callback(performance.now());
    if (framesInThisTask++ === 0) realSetTimeout(() => (framesInThisTask = 0), 0);
    if (framesInThisTask > maxFramesPerTask) realSetTimeout(runFrame, 0);
    else queueMicrotask(runFrame);
    return 0;
  };
  try {
    const motion = await importOriginal<typeof import("motion/react")>();
    motion.MotionGlobalConfig.skipAnimations = true;
    return { ...motion };
  } finally {
    globalThis.requestAnimationFrame = jsdomRequestAnimationFrame;
  }
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
