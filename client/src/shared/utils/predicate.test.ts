import { describe, expect, it } from "vitest";
import { createAndPredicate, createOrPredicate } from "./predicate";

describe("createOrPredicate", () => {
  it("returns true if at least one predicate returns true", () => {
    const isEven = (n: number) => n % 2 === 0;
    const isNegative = (n: number) => n < 0;
    const orPredicate = createOrPredicate(isEven, isNegative);

    expect(orPredicate(2)).toBe(true); // isEven
    expect(orPredicate(-1)).toBe(true); // isNegative
    expect(orPredicate(-2)).toBe(true); // both
    expect(orPredicate(3)).toBe(false); // neither
  });

  it("returns false if no predicates are provided", () => {
    const orPredicate = createOrPredicate<number>();
    expect(orPredicate(5)).toBe(false);
    expect(orPredicate(-10)).toBe(false);
  });

  it("returns the result of the single predicate if only one is provided", () => {
    const isZero = (n: number) => n === 0;
    const orPredicate = createOrPredicate(isZero);

    expect(orPredicate(0)).toBe(true);
    expect(orPredicate(1)).toBe(false);
  });

  it("works with predicates that always return false", () => {
    const alwaysFalse = () => false;
    const orPredicate = createOrPredicate(alwaysFalse, alwaysFalse);

    expect(orPredicate(42)).toBe(false);
  });

  it("works with predicates that always return true", () => {
    const alwaysTrue = () => true;
    const orPredicate = createOrPredicate(alwaysTrue, alwaysTrue);

    expect(orPredicate(42)).toBe(true);
  });
});

describe("createAndPredicate", () => {
  it("returns true only if all predicates return true", () => {
    const isEven = (n: number) => n % 2 === 0;
    const isPositive = (n: number) => n > 0;
    const andPredicate = createAndPredicate(isEven, isPositive);

    expect(andPredicate(2)).toBe(true); // both true
    expect(andPredicate(-2)).toBe(false); // isPositive false
    expect(andPredicate(3)).toBe(false); // isEven false
    expect(andPredicate(-3)).toBe(false); // both false
  });

  it("returns true if no predicates are provided", () => {
    const andPredicate = createAndPredicate<number>();
    expect(andPredicate(5)).toBe(true);
    expect(andPredicate(-10)).toBe(true);
  });

  it("returns the result of the single predicate if only one is provided", () => {
    const isZero = (n: number) => n === 0;
    const andPredicate = createAndPredicate(isZero);

    expect(andPredicate(0)).toBe(true);
    expect(andPredicate(1)).toBe(false);
  });

  it("works with predicates that always return true", () => {
    const alwaysTrue = () => true;
    const andPredicate = createAndPredicate(alwaysTrue, alwaysTrue);

    expect(andPredicate(42)).toBe(true);
  });

  it("works with predicates that always return false", () => {
    const alwaysFalse = () => false;
    const andPredicate = createAndPredicate(alwaysFalse, alwaysFalse);

    expect(andPredicate(42)).toBe(false);
  });
});
