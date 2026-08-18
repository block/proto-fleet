import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { useComponentHardware } from "./useComponentHardware";
import { useMinerStore } from "@/protoOS/store";

describe("useComponentHardware", () => {
  beforeEach(() => {
    useMinerStore.getState().resetDeviceData();
  });

  it("returns a stable control-board compatibility snapshot", () => {
    useMinerStore.getState().hardware.setControlBoard({ serial: "CU1-CB-0001", boardId: "1" });

    const { result } = renderHook(() => useComponentHardware("RIG", undefined));

    expect(result.current).toMatchObject({ serial: "CU1-CB-0001", boardId: "1" });
  });
});
