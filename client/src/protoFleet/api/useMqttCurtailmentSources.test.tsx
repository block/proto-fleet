import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { type Timestamp, TimestampSchema } from "@bufbuild/protobuf/wkt";

import { curtailmentClient } from "@/protoFleet/api/clients";
import {
  type MqttCurtailmentSource,
  MqttCurtailmentSourceRuntimeState,
  MqttCurtailmentSourceSchema,
  MqttCurtailmentSourceScopeSchema,
  MqttCurtailmentSourceScopeType,
  MqttCurtailmentSourceStatusSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import useMqttCurtailmentSources from "@/protoFleet/api/useMqttCurtailmentSources";

const {
  mockDeleteMqttCurtailmentSource,
  mockHandleAuthErrors,
  mockListMqttCurtailmentSources,
  mockUpdateMqttCurtailmentSource,
} = vi.hoisted(() => ({
  mockDeleteMqttCurtailmentSource: vi.fn(),
  mockHandleAuthErrors: vi.fn(),
  mockListMqttCurtailmentSources: vi.fn(),
  mockUpdateMqttCurtailmentSource: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  curtailmentClient: {
    deleteMqttCurtailmentSource: mockDeleteMqttCurtailmentSource,
    listMqttCurtailmentSources: mockListMqttCurtailmentSources,
    updateMqttCurtailmentSource: mockUpdateMqttCurtailmentSource,
  },
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({
    handleAuthErrors: mockHandleAuthErrors,
  }),
}));

function timestamp(isoDate: string): Timestamp {
  const date = new Date(isoDate);
  const milliseconds = date.getTime();

  return create(TimestampSchema, {
    seconds: BigInt(Math.floor(milliseconds / 1000)),
    nanos: (milliseconds % 1000) * 1_000_000,
  });
}

function mqttSource(overrides: Partial<MqttCurtailmentSource> = {}): MqttCurtailmentSource {
  const source = create(MqttCurtailmentSourceSchema, {
    sourceId: 1n,
    sourceName: "Kati MQTT",
    topic: "curtailment/site/kati",
    brokerPrimaryHost: "10.155.0.3",
    brokerSecondaryHost: "10.155.0.4",
    brokerPort: 1883,
    brokerTransport: "tcp",
    mqttUsername: "fleet",
    hasPassword: true,
    curtailMode: "FULL_FLEET",
    payloadFormat: "target_timestamp",
    scope: create(MqttCurtailmentSourceScopeSchema, {
      type: MqttCurtailmentSourceScopeType.WHOLE_ORG,
    }),
    stalenessThresholdSec: 240,
    minCurtailedDurationSec: 600,
    enabled: true,
    status: create(MqttCurtailmentSourceStatusSchema, {
      runtimeState: MqttCurtailmentSourceRuntimeState.RUNNING,
      lastTarget: "OFF",
      lastReceivedAt: timestamp("2026-06-09T15:10:00Z"),
    }),
  });

  return Object.assign(source, overrides);
}

describe("useMqttCurtailmentSources", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    mockDeleteMqttCurtailmentSource.mockReset();
    mockHandleAuthErrors.mockReset();
    mockListMqttCurtailmentSources.mockReset();
    mockUpdateMqttCurtailmentSource.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("polls sources to keep signal status current", async () => {
    mockListMqttCurtailmentSources.mockResolvedValueOnce({ sources: [mqttSource()] }).mockResolvedValueOnce({
      sources: [
        mqttSource({
          status: create(MqttCurtailmentSourceStatusSchema, {
            runtimeState: MqttCurtailmentSourceRuntimeState.RUNNING,
            lastTarget: "100",
            lastReceivedAt: timestamp("2026-06-09T15:10:30Z"),
          }),
        }),
      ],
    });

    const { result } = renderHook(() => useMqttCurtailmentSources());

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(result.current.sources[0]).toMatchObject({
      lastTarget: "OFF",
      health: "connected",
      hasPassword: true,
    });
    expect(result.current.isLoading).toBe(false);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });

    expect(mockListMqttCurtailmentSources).toHaveBeenCalledTimes(2);
    expect(result.current.sources[0]).toMatchObject({
      lastTarget: "100",
      health: "connected",
    });
    expect(result.current.isLoading).toBe(false);
  });

  it("does not poll when disabled", async () => {
    renderHook(() => useMqttCurtailmentSources(false));

    await act(async () => {
      await vi.advanceTimersByTimeAsync(20_000);
    });

    expect(curtailmentClient.listMqttCurtailmentSources).not.toHaveBeenCalled();
  });

  it("updates a source and replaces it in local hook state", async () => {
    mockListMqttCurtailmentSources.mockResolvedValueOnce({ sources: [mqttSource()] });
    mockUpdateMqttCurtailmentSource.mockResolvedValueOnce({
      source: mqttSource({
        sourceName: "Kati MQTT updated",
        topic: "curtailment/site/kati/updated",
      }),
    });

    const { result } = renderHook(() => useMqttCurtailmentSources(false));

    await act(async () => {
      await result.current.listSources();
    });
    await act(async () => {
      await result.current.updateSource("1", {
        name: "Kati MQTT updated",
        brokerPrimaryHost: "10.155.0.3",
        brokerSecondaryHost: "10.155.0.4",
        brokerPort: "1883",
        topic: "curtailment/site/kati/updated",
        username: "fleet",
        password: "",
      });
    });

    expect(mockUpdateMqttCurtailmentSource).toHaveBeenCalledWith(
      expect.objectContaining({
        sourceId: 1n,
        sourceName: "Kati MQTT updated",
        topic: "curtailment/site/kati/updated",
        brokerPrimaryHost: "10.155.0.3",
        brokerSecondaryHost: "10.155.0.4",
        brokerPort: 1883,
        mqttUsername: "fleet",
      }),
    );
    expect(mockUpdateMqttCurtailmentSource.mock.calls[0][0]).not.toHaveProperty("mqttPassword");
    expect(result.current.sources[0]).toMatchObject({
      id: "1",
      name: "Kati MQTT updated",
      topic: "curtailment/site/kati/updated",
    });
  });

  it("deletes a source and removes it from local hook state", async () => {
    mockListMqttCurtailmentSources.mockResolvedValueOnce({ sources: [mqttSource()] });
    mockDeleteMqttCurtailmentSource.mockResolvedValueOnce({});

    const { result } = renderHook(() => useMqttCurtailmentSources(false));

    await act(async () => {
      await result.current.listSources();
    });
    await act(async () => {
      await result.current.deleteSource("1");
    });

    expect(mockDeleteMqttCurtailmentSource).toHaveBeenCalledWith(
      expect.objectContaining({
        sourceId: 1n,
      }),
    );
    expect(result.current.sources).toEqual([]);
  });
});
