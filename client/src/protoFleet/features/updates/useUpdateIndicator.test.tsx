import { MemoryRouter, useLocation } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useAvailableUpdate } from "@/protoFleet/features/updates/api/useAvailableUpdate";
import { useUpdateIndicator } from "@/protoFleet/features/updates/useUpdateIndicator";

const indicatorMock = vi.hoisted(() => ({ availableVersion: "v1.3.0" as string | null }));

vi.mock("@/protoFleet/features/updates/api/useAvailableUpdate", () => ({
  useAvailableUpdate: vi.fn(({ enabled = true }: { enabled?: boolean } = {}) =>
    enabled ? indicatorMock.availableVersion : null,
  ),
}));

const Harness = ({ enabled = true }: { enabled?: boolean }) => {
  const updatePill = useUpdateIndicator({ enabled });
  const { pathname } = useLocation();

  return (
    <>
      {updatePill ? (
        <button type="button" onClick={updatePill.onClick}>
          Update {updatePill.version}
        </button>
      ) : null}
      <span data-testid="pathname">{pathname}</span>
    </>
  );
};

describe("useUpdateIndicator", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    indicatorMock.availableVersion = "v1.3.0";
  });

  it("navigates the passive indicator to authoritative update settings", () => {
    render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <Harness />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Update v1.3.0" }));

    expect(screen.getByTestId("pathname")).toHaveTextContent("/settings/updates");
    expect(vi.mocked(useAvailableUpdate)).toHaveBeenLastCalledWith({ enabled: false });
  });

  it("does not poll or render on the authoritative settings page", () => {
    render(
      <MemoryRouter initialEntries={["/settings/updates"]}>
        <Harness />
      </MemoryRouter>,
    );

    expect(screen.queryByRole("button", { name: /Update v/ })).not.toBeInTheDocument();
    expect(vi.mocked(useAvailableUpdate)).toHaveBeenCalledWith({ enabled: false });
  });

  it("does not render when the shell disables the indicator", () => {
    render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <Harness enabled={false} />
      </MemoryRouter>,
    );

    expect(screen.queryByRole("button", { name: /Update v/ })).not.toBeInTheDocument();
    expect(vi.mocked(useAvailableUpdate)).toHaveBeenCalledWith({ enabled: false });
  });

  it("does not render when no update is available", () => {
    indicatorMock.availableVersion = null;

    render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <Harness />
      </MemoryRouter>,
    );

    expect(screen.queryByRole("button", { name: /Update v/ })).not.toBeInTheDocument();
  });
});
