import { MemoryRouter, Outlet, Route, Routes } from "react-router-dom";
import { render } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import { usePersistDisplayDeepLink } from "./usePersistDisplayDeepLink";
import type { FleetOutletContext } from "@/protoFleet/features/fleetManagement/components/FleetLayout";

type DisplayMode = "grid" | "list";

type ProbeProps = {
  urlMode: DisplayMode | undefined;
  setStoredMode: (mode: DisplayMode) => void;
};

const Probe = ({ urlMode, setStoredMode }: ProbeProps) => {
  usePersistDisplayDeepLink(urlMode, setStoredMode);
  return null;
};

// Rerendering keeps the probe mounted, like a tab whose URL changes in place.
const renderInFleetShell = (urlMode: DisplayMode | undefined, activeSavedViewId?: string) => {
  const setStoredMode = vi.fn();
  const tree = (mode: DisplayMode | undefined, viewId: string | undefined) => (
    <MemoryRouter>
      <Routes>
        <Route element={<Outlet context={{ activeSavedViewId: viewId } as FleetOutletContext} />}>
          <Route index element={<Probe urlMode={mode} setStoredMode={setStoredMode} />} />
        </Route>
      </Routes>
    </MemoryRouter>
  );
  const { rerender } = render(tree(urlMode, activeSavedViewId));
  return {
    setStoredMode,
    rerender: (mode: DisplayMode | undefined, viewId?: string) => rerender(tree(mode, viewId)),
  };
};

describe("usePersistDisplayDeepLink", () => {
  test("persists a deep-linked display mode", () => {
    const { setStoredMode } = renderInFleetShell("grid");

    expect(setStoredMode).toHaveBeenCalledExactlyOnceWith("grid");
  });

  test("leaves the stored mode alone without a valid display value", () => {
    const { setStoredMode } = renderInFleetShell(undefined);

    expect(setStoredMode).not.toHaveBeenCalled();
  });

  test("leaves the stored mode alone when the active saved view supplies the display", () => {
    const { setStoredMode } = renderInFleetShell("grid", "grid-buildings");

    expect(setStoredMode).not.toHaveBeenCalled();
  });

  test("does not adopt a deleted saved view's display left behind in the URL", () => {
    const { setStoredMode, rerender } = renderInFleetShell("grid", "grid-buildings");

    rerender("grid", undefined);

    expect(setStoredMode).not.toHaveBeenCalled();
  });

  test("persists a new display value once no saved view is active", () => {
    const { setStoredMode, rerender } = renderInFleetShell("grid", "grid-buildings");

    rerender("list", undefined);

    expect(setStoredMode).toHaveBeenCalledExactlyOnceWith("list");
  });

  test("persists outside the Fleet shell, where no saved view can be active", () => {
    const setStoredMode = vi.fn();
    render(
      <MemoryRouter>
        <Probe urlMode="list" setStoredMode={setStoredMode} />
      </MemoryRouter>,
    );

    expect(setStoredMode).toHaveBeenCalledExactlyOnceWith("list");
  });
});
