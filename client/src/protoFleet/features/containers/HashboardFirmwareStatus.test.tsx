import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import HashboardFirmwareStatus from "./HashboardFirmwareStatus";
import { HashboardReadingSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

describe("HashboardFirmwareStatus", () => {
  it("shows board progress and its controller", () => {
    render(
      <HashboardFirmwareStatus
        board={create(HashboardReadingSchema, {
          metadata: {
            firmwareUpdate: {
              status: "FIRMWARE_TARGET_STATUS_FLASHING",
              progressPercent: 42,
              serverId: "ct1-b",
              observedAt: new Date().toISOString(),
            },
          },
        })}
      />,
    );
    expect(screen.getByText("Flashing (42%)")).toBeInTheDocument();
    expect(screen.getByText("ct1-b")).toBeInTheDocument();
  });
  it("labels stale failure evidence", () => {
    render(
      <HashboardFirmwareStatus
        board={create(HashboardReadingSchema, {
          metadata: {
            firmwareUpdate: {
              status: "FIRMWARE_TARGET_STATUS_FAILED",
              message: "Board verification failed",
              observedAt: "2020-01-01T00:00:00Z",
            },
          },
        })}
      />,
    );
    expect(screen.getByText("Failed · Last observed")).toBeInTheDocument();
    expect(screen.getByText("Board verification failed")).toBeInTheDocument();
  });
});
