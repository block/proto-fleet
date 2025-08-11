import { describe, expect, test, vi } from "vitest";
import {
  debounce,
  deepClone,
  formatHashrateWithUnit,
  getRowLabel,
} from "./utility";

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
    expect(Array.isArray(clonedArr[2]) && clonedArr[2][1]).toEqual(
      Array.isArray(arr[2]) && arr[2][1],
    );
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
