import { act } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import useHashboardLocationStore, {
  HashboardMap,
} from "./useHashboardLocationStore";

// Reset Zustand store between tests
const resetStore = () => {
  const { setMapping } = useHashboardLocationStore.getState();
  act(() => setMapping({}));
};

describe("useHashboardLocationStore", () => {
  beforeEach(() => {
    resetStore();
  });

  it("should set the mapping correctly", () => {
    const mapping: HashboardMap = {
      hb1: { slot: 1, bay: 1 },
      hb2: { slot: 2, bay: 1 },
      hb3: { slot: 3, bay: 2 },
    };

    act(() => {
      useHashboardLocationStore.getState().setMapping(mapping);
    });

    const storeMapping = useHashboardLocationStore.getState().mapping;
    expect(storeMapping).toEqual({
      hb1: { slot: 1, bay: 1, slotIndexByBay: 0 },
      hb2: { slot: 2, bay: 1, slotIndexByBay: 1 },
      hb3: { slot: 3, bay: 2, slotIndexByBay: 0 },
    });
  });

  it("should return the correct slot for a given hbSn", () => {
    const mapping: HashboardMap = {
      hb1: { slot: 1, bay: 1 },
      hb2: { slot: 2, bay: 1 },
    };

    act(() => {
      useHashboardLocationStore.getState().setMapping(mapping);
    });

    const slot = useHashboardLocationStore.getState().getSlotByHbSn("hb1");
    expect(slot).toBe(1);

    const undefinedSlot = useHashboardLocationStore
      .getState()
      .getSlotByHbSn("hb3");
    expect(undefinedSlot).toBeUndefined();
  });

  it("should return the correct bay for a given hbSn", () => {
    const mapping: HashboardMap = {
      hb1: { slot: 1, bay: 1 },
      hb2: { slot: 2, bay: 2 },
    };

    act(() => {
      useHashboardLocationStore.getState().setMapping(mapping);
    });

    const bay = useHashboardLocationStore.getState().getBayByHbSn("hb2");
    expect(bay).toBe(2);

    const undefinedBay = useHashboardLocationStore
      .getState()
      .getBayByHbSn("hb3");
    expect(undefinedBay).toBeUndefined();
  });

  it("should calculate the correct bay count", () => {
    const mapping: HashboardMap = {
      hb1: { slot: 1, bay: 1 },
      hb2: { slot: 2, bay: 2 },
      hb3: { slot: 3, bay: 3 },
    };

    act(() => {
      useHashboardLocationStore.getState().setMapping(mapping);
    });

    const bayCount = useHashboardLocationStore.getState().getBayCount();
    expect(bayCount).toBe(3);
  });

  it("should calculate the correct slot index by bay", () => {
    const mapping: HashboardMap = {
      hb1: { slot: 1, bay: 1 },
      hb2: { slot: 2, bay: 1 },
      hb3: { slot: 3, bay: 2 },
      hb4: { slot: 4, bay: 2 },
    };

    act(() => {
      useHashboardLocationStore.getState().setMapping(mapping);
    });

    const slotIndex1 = useHashboardLocationStore
      .getState()
      .getBaySlotIndexByHbSn("hb1");
    expect(slotIndex1).toBe(0);

    const slotIndex2 = useHashboardLocationStore
      .getState()
      .getBaySlotIndexByHbSn("hb2");
    expect(slotIndex2).toBe(1);

    const slotIndex3 = useHashboardLocationStore
      .getState()
      .getBaySlotIndexByHbSn("hb3");
    expect(slotIndex3).toBe(0);

    const slotIndex4 = useHashboardLocationStore
      .getState()
      .getBaySlotIndexByHbSn("hb4");
    expect(slotIndex4).toBe(1);

    const undefinedSlotIndex = useHashboardLocationStore
      .getState()
      .getBaySlotIndexByHbSn("hb5");
    expect(undefinedSlotIndex).toBeUndefined();
  });
});
