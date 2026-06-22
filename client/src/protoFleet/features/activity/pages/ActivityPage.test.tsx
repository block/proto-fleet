import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import ActivityPage from "./ActivityPage";

const canReadActivityMock = vi.hoisted(() => ({ current: false }));
const useActivityMock = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: () => canReadActivityMock.current,
}));

vi.mock("@/protoFleet/api/useActivity", () => ({
  useActivity: useActivityMock,
}));

vi.mock("@/protoFleet/api/useActivityFilterOptions", () => ({
  useActivityFilterOptions: () => ({ eventTypes: [], scopeTypes: [], users: [] }),
}));

vi.mock("@/protoFleet/api/useExportActivity", () => ({
  useExportActivity: () => ({ exportCsv: vi.fn(), isExportingCsv: false }),
}));

vi.mock("@/protoFleet/features/activity/components/ActivityFilters", () => ({
  default: () => <div data-testid="activity-filters" />,
}));

vi.mock("@/protoFleet/features/activity/components/ActivityTable", () => ({
  default: () => <div data-testid="activity-table" />,
}));

const LocationProbe = () => {
  const location = useLocation();
  return <div data-testid="location-probe">{location.pathname}</div>;
};

const renderActivityRoute = () =>
  render(
    <MemoryRouter initialEntries={["/activity"]}>
      <Routes>
        <Route path="/" element={<div data-testid="home-page">Home</div>} />
        <Route path="/activity" element={<ActivityPage />} />
      </Routes>
      <LocationProbe />
    </MemoryRouter>,
  );

describe("ActivityPage permission guard", () => {
  beforeEach(() => {
    canReadActivityMock.current = false;
    useActivityMock.mockReset();
    useActivityMock.mockReturnValue({
      activities: [],
      totalCount: 0,
      isLoading: false,
      error: null,
      hasMore: false,
      loadMore: vi.fn(),
    });
  });

  test("redirects without calling activity data hooks when org activity:read is missing", async () => {
    renderActivityRoute();

    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/"));
    expect(screen.getByTestId("home-page")).toBeInTheDocument();
    expect(useActivityMock).not.toHaveBeenCalled();
  });

  test("renders activity content when org activity:read is present", () => {
    canReadActivityMock.current = true;

    renderActivityRoute();

    expect(screen.getByTestId("location-probe").textContent).toBe("/activity");
    expect(screen.getByTestId("activity-table")).toBeInTheDocument();
    expect(useActivityMock).toHaveBeenCalledOnce();
  });
});
