import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { usePlantAlarmErrors } from "./usePlantAlarmErrors";
import {
  ErrorMessageSchema,
  MinerError,
  QueryResponseSchema,
  ResultView,
  Severity,
} from "@/protoFleet/api/generated/errors/v1/errors_pb";

const api = vi.hoisted(() => ({ query: vi.fn() }));
vi.mock("@/protoFleet/api/clients", () => ({ errorQueryClient: api }));
const error = (componentId: string) =>
  create(ErrorMessageSchema, { errorId: componentId, componentId, severity: Severity.MAJOR, summary: componentId });
const page = (items: string[], next = "") =>
  create(QueryResponseSchema, {
    result: { case: "errors", value: { items: items.map(error) } },
    nextPageToken: next,
  });

afterEach(() => vi.resetAllMocks());

it("queries open vendor-unmapped errors for the container and keeps plant alarm components across pages", async () => {
  api.query
    .mockResolvedValueOnce(page(["mac:aa", "plant-alarm:FIRE"], "next"))
    .mockResolvedValueOnce(page(["plant-alarm:PUMP"]));
  const { result } = renderHook(() => usePlantAlarmErrors("ct1", "1"));
  await waitFor(() => expect(result.current.loaded).toBe(true));
  expect(result.current.alarms.map((alarm) => alarm.name)).toEqual(["plant-alarm:FIRE", "plant-alarm:PUMP"]);
  expect(api.query).toHaveBeenCalledTimes(2);
  const request = api.query.mock.calls[0][0];
  expect(request).toMatchObject({
    resultView: ResultView.ERROR,
    filter: {
      simple: { deviceIdentifiers: ["ct1"], canonicalErrors: [MinerError.VENDOR_ERROR_UNMAPPED] },
      includeClosed: false,
    },
  });
  expect(api.query.mock.calls[1][0].pageToken).toBe("next");
});

it("reports a failed query without throwing", async () => {
  api.query.mockRejectedValue(new Error("unavailable"));
  const { result } = renderHook(() => usePlantAlarmErrors("ct1", "1"));
  await waitFor(() => expect(result.current.failed).toBe(true));
  expect(result.current.alarms).toEqual([]);
});

it("does not query when disabled", () => {
  renderHook(() => usePlantAlarmErrors("ct1", "1", false));
  expect(api.query).not.toHaveBeenCalled();
});
