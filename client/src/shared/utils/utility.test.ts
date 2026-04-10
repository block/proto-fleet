import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { copyToClipboard, debounce, deepClone, formatHashrateWithUnit, formatTempRange, getRowLabel } from "./utility";

describe("deepClone", () => {
  test("should create a deep copy of an object", () => {
    const obj = { a: 1, b: { c: 2 } };
    const clonedObj = deepClone(obj);

    expect(clonedObj).toEqual(obj);
    expect(clonedObj.b).toEqual(obj.b);
    expect(clonedObj.b.c).toEqual(obj.b.c);
  });
  test("should create a deep copy of an array", () => {
    const arr: (number | number[])[] = [1, 2, [3, 4]];
    const clonedArr = deepClone(arr);

    expect(clonedArr).toEqual(arr);
    expect(Array.isArray(clonedArr[2]) && clonedArr[2][1]).toEqual(Array.isArray(arr[2]) && arr[2][1]);
  });
  test("should create a deep copy of a string", () => {
    const str = "hello";
    const clonedStr = deepClone(str);

    expect(clonedStr).toEqual(str);
  });
  test("should create a deep copy of a number", () => {
    const num = 42;
    const clonedNum = deepClone(num);

    expect(clonedNum).toEqual(num);
  });
  test("should create a deep copy of a boolean", () => {
    const bool = true;
    const clonedBool = deepClone(bool);

    expect(clonedBool).toEqual(bool);
  });
  test("should create a deep copy of a null value", () => {
    const nullVal = null;
    const clonedNull = deepClone(nullVal);

    expect(clonedNull).toEqual(nullVal);
  });
  test("should create a deep copy of an undefined value", () => {
    const undefinedVal = undefined;
    const clonedUndefined = deepClone(undefinedVal);

    expect(clonedUndefined).toEqual(undefinedVal);
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

describe("formatHashrateWithUnit", () => {
  test("should return TH/S for values <= 1000", () => {
    expect(formatHashrateWithUnit(0)).toEqual({ value: 0, unit: "TH/S" });
    expect(formatHashrateWithUnit(500)).toEqual({ value: 500, unit: "TH/S" });
    expect(formatHashrateWithUnit(1000)).toEqual({ value: 1000, unit: "TH/S" });
  });

  test("should return PH/S for values > 1000", () => {
    expect(formatHashrateWithUnit(1001)).toEqual({
      value: 1.001,
      unit: "PH/S",
    });
    expect(formatHashrateWithUnit(2000)).toEqual({ value: 2, unit: "PH/S" });
    expect(formatHashrateWithUnit(5500)).toEqual({ value: 5.5, unit: "PH/S" });
  });

  test("should handle undefined/null values", () => {
    expect(formatHashrateWithUnit()).toEqual({ value: 0, unit: "TH/S" });
  });
});

describe("formatTempRange", () => {
  test("formats Celsius range with one decimal", () => {
    expect(formatTempRange(20.1, 65.5, "C")).toBe("20.1 °C – 65.5 °C");
  });

  test("formats Fahrenheit range by converting from Celsius", () => {
    expect(formatTempRange(0, 100, "F")).toBe("32.0 °F – 212.0 °F");
  });

  test("uses correct degree sign (U+00B0) and en dash", () => {
    const result = formatTempRange(30, 40, "C");
    expect(result).toContain("°"); // U+00B0 degree sign
    expect(result).toContain("–"); // en dash
    expect(result).not.toContain("º"); // not U+00BA ordinal indicator
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
