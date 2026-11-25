import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, test } from "vitest";

import useMinerStore from "../useMinerStore";
import { useIsMiningDriverRunning, useIsWebServerRunning } from "./useSystemInfo";
import { toCanonicalSystemInfo } from "@/protoOS/api/adapters/mdkV1/normalize";

describe("web-server reachability", () => {
  beforeEach(() => {
    useMinerStore.getState().resetDeviceData();
  });

  test.each([undefined, {}, { version: "1.0" }, { name: "miner-api-server" }])(
    "accepts a successful v1 response with web_server=%j",
    (web_server) => {
      // Both web_server and SWInfo.name are optional in the MDK v1 OpenAPI contract.
      const systemInfo = toCanonicalSystemInfo({ product_name: "Proto Rig", web_server });
      const { result } = renderHook(() => ({
        webServer: useIsWebServerRunning(),
        miningDriver: useIsMiningDriverRunning(),
      }));

      act(() => useMinerStore.getState().systemInfo.setSystemInfo(systemInfo));

      expect(result.current.webServer).toBe(true);
      expect(result.current.miningDriver).toBe(false);
    },
  );

  test("stays offline until the first request succeeds, then stays online during refreshes", () => {
    const { result } = renderHook(() => useIsWebServerRunning());
    expect(result.current).toBe(false);

    act(() => useMinerStore.getState().systemInfo.setPending(true));
    expect(result.current).toBe(false);

    act(() => useMinerStore.getState().systemInfo.setSystemInfo(toCanonicalSystemInfo({})));
    expect(result.current).toBe(true);

    act(() => useMinerStore.getState().systemInfo.setPending(true));
    expect(result.current).toBe(true);

    act(() => useMinerStore.getState().systemInfo.setPending(true));
    expect(result.current).toBe(true);
  });

  test("goes offline on failure despite cached metadata and recovers only after a successful retry", () => {
    const systemInfo = toCanonicalSystemInfo({ web_server: { name: "miner-api-server" } });
    useMinerStore.getState().systemInfo.setSystemInfo(systemInfo);
    const { result } = renderHook(() => useIsWebServerRunning());
    expect(result.current).toBe(true);

    act(() => useMinerStore.getState().systemInfo.setError("Connection refused"));
    expect(useMinerStore.getState().systemInfo.data).toEqual(systemInfo);
    expect(result.current).toBe(false);

    act(() => useMinerStore.getState().systemInfo.setPending(true));
    expect(result.current).toBe(false);

    act(() => useMinerStore.getState().systemInfo.setSystemInfo(toCanonicalSystemInfo({})));
    expect(result.current).toBe(true);
  });

  test("does not carry reachability to another device", () => {
    useMinerStore.getState().systemInfo.setSystemInfo(toCanonicalSystemInfo({}));
    const { result } = renderHook(() => useIsWebServerRunning());
    expect(result.current).toBe(true);

    act(() => useMinerStore.getState().resetDeviceData());
    expect(result.current).toBe(false);

    act(() => useMinerStore.getState().systemInfo.setPending(true));
    expect(result.current).toBe(false);
  });
});
