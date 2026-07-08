import { MemoryRouter } from "react-router-dom";
import { render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import AppLayout from "./AppLayout";
import { navigationMenuTypes } from "@/protoOS/components/NavigationMenu";
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";
import useMinerStore from "@/protoOS/store/useMinerStore";

describe("AppLayout", () => {
  beforeEach(() => {
    useMinerStore.getState().systemInfo.setSystemInfo({
      product_name: "Proto Rig",
      os: { version: "1.8.0" },
    });
    useMinerStore.getState().networkInfo.setNetworkInfo({
      ip: "192.168.2.14",
      mac: "02:00:00:07:3A:11",
    });
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

    expect(within(screen.getByTestId("miner-name-info-item")).queryByText("Proto Rig")).not.toBeInTheDocument();
  });
});
