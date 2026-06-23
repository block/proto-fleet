import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import SiteDetailPage from "./SiteDetailPage";
import { SiteSchema, type SiteWithCounts, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const listSitesMock = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/sites", async () => {
  const actual = await vi.importActual<typeof import("@/protoFleet/api/sites")>("@/protoFleet/api/sites");
  return {
    ...actual,
    useSites: () => ({
      listSites: listSitesMock,
    }),
  };
});

vi.mock("@/protoFleet/features/sites/components/SiteModals", () => ({
  __esModule: true,
  default: () => null,
}));

vi.mock("@/protoFleet/features/sites/hooks/useSiteModals", () => ({
  useSiteModals: () => ({
    openManageEdit: vi.fn(),
  }),
}));

vi.mock("@/protoFleet/features/buildings/components/BuildingModals", () => ({
  __esModule: true,
  default: () => null,
}));

vi.mock("@/protoFleet/features/buildings/hooks/useBuildingModals", () => ({
  useBuildingModals: () => ({
    openDetailsCreate: vi.fn(),
    openDetailsEdit: vi.fn(),
  }),
}));

const LocationProbe = () => {
  const location = useLocation();
  return <div data-testid="location-probe">{`${location.pathname}${location.search}`}</div>;
};

const makeSite = (id: bigint, name: string) =>
  create(SiteWithCountsSchema, {
    site: create(SiteSchema, {
      id,
      name,
      country: "US",
    }),
    deviceCount: 0n,
    buildingCount: 0n,
    rackCount: 0n,
  });

const renderPage = (initialEntry = "/sites/7") =>
  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/sites/:id" element={<SiteDetailPage />} />
        <Route path="*" element={<LocationProbe />} />
      </Routes>
    </MemoryRouter>,
  );

describe("SiteDetailPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useFleetStore.setState((state) => {
      state.ui.activeSite = DEFAULT_ACTIVE_SITE;
    });
    listSitesMock.mockImplementation(({ onSuccess }: { onSuccess: (sites: SiteWithCounts[]) => void }) =>
      onSuccess([makeSite(7n, "Dallas"), makeSite(8n, "Austin")]),
    );
  });

  it("preserves the selected site when a site detail mismatch redirects back to Fleet", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "8" };
    });

    renderPage();

    await waitFor(() => expect(screen.getByTestId("location-probe")).toHaveTextContent("/8/fleet"));
  });
});
