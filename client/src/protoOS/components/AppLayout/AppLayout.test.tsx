import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import AppLayout from "./AppLayout";
import { toCanonicalContainer } from "@/protoOS/api/adapters/mdkV2/normalize";
import DefaultContentLayout from "@/protoOS/components/ContentLayout/DefaultContentLayout";
import FullScreenContentLayout from "@/protoOS/components/ContentLayout/FullScreenContentLayout";
import ListContentLayout from "@/protoOS/components/ContentLayout/ListContentLayout";
import SettingsContentLayout from "@/protoOS/components/ContentLayout/SettingsContentLayout";

import { navigationMenuTypes } from "@/protoOS/components/NavigationMenu";
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";
import { FeedMappingProvider } from "@/protoOS/features/feedMapping/FeedMappingProvider";
import { asDeviceId } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

describe("AppLayout", () => {
  beforeEach(() => {
    useMinerStore.getState().resetDeviceData();
    useMinerStore.getState().systemInfo.setSystemInfo({
      identity: {
        deviceId: asDeviceId("device:test"),
        deviceType: "rig",
        adapterId: "mdk-v1",
        productName: "Proto Rig",
        macAddresses: [],
      },
      moduleModel: "r2",
      operatingSystem: { version: "1.8.0" },
    });
    useMinerStore
      .getState()
      .networkInfo.setNetworkInfo(
        new Map([
          ["primary", { id: "primary", ipAddress: "192.168.2.14", macAddress: "02:00:00:07:3A:11", isPrimary: true }],
        ]),
      );
  });

  it("uses host metadata for the navigation info panel in fleet-hosted mode", () => {
    render(
      <MemoryRouter>
        <MinerHostingProvider
          mode="fleet"
          metadata={{
            minerName: "Antminer S21",
            ipAddress: "10.0.0.42",
            firmwareVersion: "2026.1",
            macAddress: "AA:BB:CC:DD:EE:FF",
            site: "Rockdale",
            building: "Building A",
            rack: "Rack 12",
          }}
        >
          <AppLayout title="Home" type={navigationMenuTypes.app} customHeaderButtons={<div />}>
            <div>Content</div>
          </AppLayout>
        </MinerHostingProvider>
      </MemoryRouter>,
    );

    expect(within(screen.getByTestId("miner-name-info-item")).getByText("Antminer S21")).toBeInTheDocument();
    expect(within(screen.getByTestId("ip-address-info-item")).getByText("10.0.0.42")).toBeInTheDocument();
    expect(within(screen.getByTestId("version-info-item")).getByText("2026.1")).toBeInTheDocument();
    expect(within(screen.getByTestId("mac-address-info-item")).getByText("AA:BB:CC:DD:EE:FF")).toBeInTheDocument();

    const locationItem = within(screen.getByTestId("location-info-item"));
    expect(locationItem.getByText("Rockdale")).toBeInTheDocument();
    expect(locationItem.getByText("Building A")).toBeInTheDocument();
    expect(locationItem.getByText("Rack 12")).toBeInTheDocument();

    expect(within(screen.getByTestId("miner-name-info-item")).queryByText("Proto Rig")).not.toBeInTheDocument();
  });

  it("omits placement levels the miner is not assigned to", () => {
    render(
      <MemoryRouter>
        <MinerHostingProvider mode="fleet" metadata={{ minerName: "Antminer S21", site: "Rockdale" }}>
          <AppLayout title="Home" type={navigationMenuTypes.app} customHeaderButtons={<div />}>
            <div>Content</div>
          </AppLayout>
        </MinerHostingProvider>
      </MemoryRouter>,
    );

    expect(within(screen.getByTestId("location-info-item")).getByText("Rockdale")).toBeInTheDocument();
  });

  it("hides the location row when fleet-hosted with no placement", () => {
    render(
      <MemoryRouter>
        <MinerHostingProvider mode="fleet" metadata={{ minerName: "Antminer S21" }}>
          <AppLayout title="Home" type={navigationMenuTypes.app} customHeaderButtons={<div />}>
            <div>Content</div>
          </AppLayout>
        </MinerHostingProvider>
      </MemoryRouter>,
    );

    expect(screen.queryByTestId("location-info-item")).not.toBeInTheDocument();
  });

  it("hides the location row when not fleet-hosted", () => {
    render(
      <MemoryRouter>
        <MinerHostingProvider mode="direct" metadata={{ site: "Rockdale", building: "Building A", rack: "Rack 12" }}>
          <AppLayout title="Home" type={navigationMenuTypes.app} customHeaderButtons={<div />}>
            <div>Content</div>
          </AppLayout>
        </MinerHostingProvider>
      </MemoryRouter>,
    );

    expect(screen.queryByTestId("location-info-item")).not.toBeInTheDocument();
  });

  it.each([
    ["default", DefaultContentLayout],
    ["list", ListContentLayout],
    ["full screen", FullScreenContentLayout],
    ["settings", SettingsContentLayout],
  ] as const)("places one combined callout above the %s page header", async (_, ContentLayout) => {
    const snapshot = toCanonicalContainer(
      {
        containerMap: {
          tanks: [{ id: 0, modules: [{ id: 0, slots: [{ hashboard: { macAddress: "aa:bb:cc:00:00:01" } }] }] }],
        },
      },
      {},
      {},
      "ct1",
    );
    useMinerStore.getState().hardware.replaceHardware(snapshot.hardware);
    const mapping = {
      assess: vi.fn().mockResolvedValue({
        mode: "manual",
        hardMappingAvailable: true,
        configurationReasons: [],
        changes: [],
        boards: [],
      }),
      active: vi.fn().mockResolvedValue(null),
      get: vi.fn(),
      plan: vi.fn(),
      start: vi.fn(),
      cancel: vi.fn(),
      verify: vi.fn(),
    };
    render(
      <MemoryRouter>
        <FeedMappingProvider mapping={mapping} deviceId="ct1">
          <AppLayout
            title="Review"
            type={navigationMenuTypes.app}
            customHeaderButtons={<div />}
            ContentLayout={ContentLayout}
          >
            <h1>Page heading</h1>
          </AppLayout>
        </FeedMappingProvider>
      </MemoryRouter>,
    );
    const refresh = await screen.findByRole("button", { name: "Refresh mapping" });
    const callout = screen.getByTestId("callout");
    expect(
      callout.compareDocumentPosition(screen.getByRole("heading", { name: "Page heading" })) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    fireEvent.click(refresh);
    const dialog = await screen.findByRole("dialog", { name: "Hashboard mapping" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
    fireEvent.click(screen.getByRole("button", { name: "View hashboards" }));
    expect(await screen.findByRole("dialog", { name: "Unlocated hashboards" })).toBeInTheDocument();
    expect(mapping.start).not.toHaveBeenCalled();
  });
});
