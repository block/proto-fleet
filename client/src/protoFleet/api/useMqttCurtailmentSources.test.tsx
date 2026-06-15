import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { type Timestamp, TimestampSchema } from "@bufbuild/protobuf/wkt";

import { curtailmentClient } from "@/protoFleet/api/clients";
import {
  type MqttCurtailmentSource,
  MqttCurtailmentSourceRuntimeState,
  MqttCurtailmentSourceSchema,
  MqttCurtailmentSourceStatusSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import useMqttCurtailmentSources from "@/protoFleet/api/useMqttCurtailmentSources";

const {
  mockDeleteMqttCurtailmentSource,
  mockHandleAuthErrors,
  mockListMqttCurtailmentSources,
  mockTestMqttCurtailmentSourceConnection,
  mockUpdateMqttCurtailmentSource,
} = vi.hoisted(() => ({
  mockDeleteMqttCurtailmentSource: vi.fn(),
  mockHandleAuthErrors: vi.fn(),
  mockListMqttCurtailmentSources: vi.fn(),
  mockTestMqttCurtailmentSourceConnection: vi.fn(),
  mockUpdateMqttCurtailmentSource: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  curtailmentClient: {
    deleteMqttCurtailmentSource: mockDeleteMqttCurtailmentSource,
    listMqttCurtailmentSources: mockListMqttCurtailmentSources,
    testMqttCurtailmentSourceConnection: mockTestMqttCurtailmentSourceConnection,
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
    sourceName: "Site Alpha MQTT",
    topic: "curtailment/site-alpha/target",
    brokerPrimaryHost: "site-alpha-primary.broker.test",
    brokerSecondaryHost: "site-alpha-secondary.broker.test",
    brokerPort: 11883,
    brokerTransport: "tcp",
    mqttUsername: "fleet",
    hasPassword: true,
    payloadFormat: "target_timestamp",
    stalenessThresholdSec: 240,
    enabled: true,
    status: create(MqttCurtailmentSourceStatusSchema, {
      runtimeState: MqttCurtailmentSourceRuntimeState.RUNNING,
      lastTarget: "OFF",
      lastReceivedAt: timestamp("2026-06-09T15:10:00Z"),
    }),
  });

  return Object.assign(source, overrides);
}

const testSourceFormValues = {
  name: "Site Alpha MQTT",
  brokerPrimaryHost: "site-alpha-primary.broker.test",
  brokerSecondaryHost: "site-alpha-secondary.broker.test",
  brokerPort: "11883",
  topic: "curtailment/site-alpha/target",
  username: "fleet",
  password: "secret",
};

describe("useMqttCurtailmentSources", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    mockDeleteMqttCurtailmentSource.mockReset();
    mockHandleAuthErrors.mockReset();
    mockListMqttCurtailmentSources.mockReset();
    mockTestMqttCurtailmentSourceConnection.mockReset();
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
    expect(result.current.sources[0].lastSeen).toMatch(/:\d{2}:00(?:AM|PM)$/);
    expect(result.current.isLoading).toBe(false);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });

    expect(mockListMqttCurtailmentSources).toHaveBeenCalledTimes(2);
    expect(result.current.sources[0]).toMatchObject({
      lastTarget: "100",
      health: "connected",
    });
    expect(result.current.sources[0].lastSeen).toMatch(/:\d{2}:30(?:AM|PM)$/);
    expect(result.current.isLoading).toBe(false);
  });

  it("does not poll when disabled", async () => {
    renderHook(() => useMqttCurtailmentSources(false));

    await act(async () => {
      await vi.advanceTimersByTimeAsync(20_000);
    });

    expect(curtailmentClient.listMqttCurtailmentSources).not.toHaveBeenCalled();
  });

  it("shows waiting for signal before a source receives its first MQTT signal", async () => {
    mockListMqttCurtailmentSources.mockResolvedValueOnce({
      sources: [
        mqttSource({
          status: create(MqttCurtailmentSourceStatusSchema, {
            runtimeState: MqttCurtailmentSourceRuntimeState.STOPPED,
            stale: true,
          }),
        }),
      ],
    });

    const { result } = renderHook(() => useMqttCurtailmentSources(false));

    await act(async () => {
      await result.current.listSources();
    });

    expect(result.current.sources[0]).toMatchObject({
      lastTarget: "-",
      lastSeen: "-",
      health: "waitingForSignal",
    });
  });

  it("tests a source connection with the current form values", async () => {
    mockTestMqttCurtailmentSourceConnection.mockResolvedValueOnce({ ok: true, results: [] });

    const { result } = renderHook(() => useMqttCurtailmentSources(false));

    await act(async () => {
      await result.current.testConnection(testSourceFormValues);
    });

    expect(mockTestMqttCurtailmentSourceConnection).toHaveBeenCalledWith(
      expect.objectContaining({
        topic: "curtailment/site-alpha/target",
        brokerPrimaryHost: "site-alpha-primary.broker.test",
        brokerSecondaryHost: "site-alpha-secondary.broker.test",
        brokerPort: 11883,
        brokerTransport: "tcp",
        mqttUsername: "fleet",
        mqttPassword: "secret",
        payloadFormat: "target_timestamp",
      }),
    );
    expect(result.current.isTestingConnection).toBe(false);
  });

  it("rejects when a source connection test returns a failed result", async () => {
    mockTestMqttCurtailmentSourceConnection.mockResolvedValueOnce({
      ok: false,
      results: [{ broker: "site-alpha-primary.broker.test", brokerRole: "primary", connected: false }],
    });

    const { result } = renderHook(() => useMqttCurtailmentSources(false));

    await expect(result.current.testConnection(testSourceFormValues)).rejects.toThrow(
      "Failed to connect to the MaestroOS MQTT broker.",
    );
    expect(mockHandleAuthErrors).toHaveBeenCalled();
    expect(result.current.isTestingConnection).toBe(false);
  });

  it("updates a source and replaces it in local hook state", async () => {
    mockListMqttCurtailmentSources.mockResolvedValueOnce({ sources: [mqttSource()] });
    mockUpdateMqttCurtailmentSource.mockResolvedValueOnce({
      source: mqttSource({
        sourceName: "Site Alpha MQTT updated",
        topic: "curtailment/site-alpha/target/updated",
      }),
    });

    const { result } = renderHook(() => useMqttCurtailmentSources(false));

    await act(async () => {
      await result.current.listSources();
    });
    await act(async () => {
      await result.current.updateSource("1", {
        name: "Site Alpha MQTT updated",
        brokerPrimaryHost: "site-alpha-primary.broker.test",
        brokerSecondaryHost: "site-alpha-secondary.broker.test",
        brokerPort: "11883",
        topic: "curtailment/site-alpha/target/updated",
        username: "fleet",
        password: "",
      });
    });

    expect(mockUpdateMqttCurtailmentSource).toHaveBeenCalledWith(
      expect.objectContaining({
        sourceId: 1n,
        sourceName: "Site Alpha MQTT updated",
        topic: "curtailment/site-alpha/target/updated",
        brokerPrimaryHost: "site-alpha-primary.broker.test",
        brokerSecondaryHost: "site-alpha-secondary.broker.test",
        brokerPort: 11883,
        mqttUsername: "fleet",
      }),
    );
    expect(mockUpdateMqttCurtailmentSource.mock.calls[0][0]).not.toHaveProperty("mqttPassword");
    expect(result.current.sources[0]).toMatchObject({
      id: "1",
      name: "Site Alpha MQTT updated",
      topic: "curtailment/site-alpha/target/updated",
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
