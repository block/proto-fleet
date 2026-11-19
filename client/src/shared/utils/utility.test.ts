import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { copyToClipboard, debounce, deepClone, getRowLabel } from "./utility";

describe("deepClone", () => {
  test("copies each level, so a change to the copy does not change the source", () => {
    const source = { a: 1, b: { c: 2 }, list: [1, [2, 3]] };
    const clone = deepClone(source);

    expect(clone).toEqual(source);
    expect(clone).not.toBe(source);
    expect(clone.b).not.toBe(source.b);
    expect(clone.list).not.toBe(source.list);
    expect(clone.list[1]).not.toBe(source.list[1]);

    clone.b.c = 99;
    clone.list[1][0] = 99;
    expect(source).toEqual({ a: 1, b: { c: 2 }, list: [1, [2, 3]] });
  });

  test.each([
    ["a string", "hello"],
    ["a number", 42],
    ["a boolean", true],
    ["null", null],
    ["undefined", undefined],
  ])("returns %s unchanged", (_name, value) => {
    expect(deepClone(value)).toBe(value);
  });

  test("converts a bigint to a number", () => {
    expect(deepClone({ total: 10n, nested: [2n] })).toEqual({ total: 10, nested: [2] });
  });
});

describe("debounce", () => {
  test("should debounce function calls", () => {
    vi.useFakeTimers();

    const callback = vi.fn();
    const debouncedFn = debounce(callback);

    // Call the debounced function multiple times within the debounce interval
    debouncedFn();
    debouncedFn();
    debouncedFn();

    // The callback should not have been called yet
    expect(callback).not.toBeCalled();

    // Fast-forward time by 500ms
    vi.advanceTimersByTime(500);

    // The callback should have been called only once
    expect(callback).toBeCalledTimes(1);

    // Reset the timers
    vi.useRealTimers();
  });

  test("should not call the callback if cancelled", () => {
    vi.useFakeTimers();

    const callback = vi.fn();
    const debouncedFn = debounce(callback);

    // Call the debounced function multiple times within the debounce interval
    debouncedFn();
    debouncedFn();
    debouncedFn();

    // The callback should not have been called yet
    expect(callback).not.toBeCalled();

    // Cancel debounce
    debouncedFn.cancel();

    // Fast-forward time by 500ms
    vi.advanceTimersByTime(500);

    // The callback should have been called only once
    expect(callback).toBeCalledTimes(0);

    // Reset the timers
    vi.useRealTimers();
  });
});

describe("getRowLabel", () => {
  test("should return the alphabet character for the given row number", () => {
    expect(getRowLabel(0)).toBe("A");
    expect(getRowLabel(1)).toBe("B");
    expect(getRowLabel(2)).toBe("C");
    expect(getRowLabel(3)).toBe("D");
    expect(getRowLabel(4)).toBe("E");
    expect(getRowLabel(5)).toBe("F");
    expect(getRowLabel(6)).toBe("G");
    expect(getRowLabel(7)).toBe("H");
    expect(getRowLabel(8)).toBe("I");
    expect(getRowLabel(9)).toBe("J");
  });
});

describe("copyToClipboard", () => {
  let originalClipboard: Clipboard | undefined;
  let originalExecCommand: typeof document.execCommand;

  beforeEach(() => {
    originalClipboard = navigator.clipboard;
    originalExecCommand = document.execCommand;
  });

  afterEach(() => {
    Object.defineProperty(navigator, "clipboard", {
      value: originalClipboard,
      writable: true,
      configurable: true,
    });
    document.execCommand = originalExecCommand;
  });

  test("should use navigator.clipboard.writeText in secure context", async () => {
    const mockWriteText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText: mockWriteText },
      writable: true,
      configurable: true,
    });
    Object.defineProperty(window, "isSecureContext", {
      value: true,
      writable: true,
      configurable: true,
    });

    await copyToClipboard("test text");

    expect(mockWriteText).toHaveBeenCalledWith("test text");
  });

  test("should fall back to execCommand when not in secure context", async () => {
    Object.defineProperty(navigator, "clipboard", {
      value: undefined,
      writable: true,
      configurable: true,
    });
    Object.defineProperty(window, "isSecureContext", {
      value: false,
      writable: true,
      configurable: true,
    });

    const mockExecCommand = vi.fn().mockReturnValue(true);
    document.execCommand = mockExecCommand;

    await copyToClipboard("fallback text");

    expect(mockExecCommand).toHaveBeenCalledWith("copy");
  });

  test("should throw error when execCommand fails", async () => {
    Object.defineProperty(navigator, "clipboard", {
      value: undefined,
      writable: true,
      configurable: true,
    });
    Object.defineProperty(window, "isSecureContext", {
      value: false,
      writable: true,
      configurable: true,
    });

    const mockExecCommand = vi.fn().mockReturnValue(false);
    document.execCommand = mockExecCommand;

    await expect(copyToClipboard("test")).rejects.toThrow("Copy command was unsuccessful");
  });

  test("should clean up textarea element after fallback copy", async () => {
    Object.defineProperty(navigator, "clipboard", {
      value: undefined,
      writable: true,
      configurable: true,
    });
    Object.defineProperty(window, "isSecureContext", {
      value: false,
      writable: true,
      configurable: true,
    });

    const mockExecCommand = vi.fn().mockReturnValue(true);
    document.execCommand = mockExecCommand;

    const initialChildCount = document.body.childElementCount;

    await copyToClipboard("cleanup test");

    expect(document.body.childElementCount).toBe(initialChildCount);
  });
});
