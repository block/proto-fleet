import { create, fromJsonString, toJsonString } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { type Route } from "@playwright/test";
import {
  ListServerLogsRequestSchema,
  ListServerLogsResponseSchema,
  LogEntrySchema,
  type LogLevel,
} from "@/protoFleet/api/generated/serverlog/v1/serverlog_pb";

export function createServerLogEntry({
  id,
  level,
  message,
  source,
  time,
  attrs = [],
}: {
  id: bigint;
  level: LogLevel;
  message: string;
  source: string;
  time: Date;
  attrs?: Array<{ key: string; value: string }>;
}) {
  return create(LogEntrySchema, {
    id,
    level,
    message,
    source,
    attrs,
    time: create(TimestampSchema, {
      seconds: BigInt(Math.floor(time.getTime() / 1000)),
      nanos: 0,
    }),
  });
}

export type ServerLogEntry = ReturnType<typeof createServerLogEntry>;

export function parseServerLogsRequest(route: Route) {
  return fromJsonString(ListServerLogsRequestSchema, route.request().postData() ?? "{}");
}

export function fulfillServerLogs(route: Route, entries: ServerLogEntry[], latestId: bigint) {
  return route.fulfill({
    status: 200,
    contentType: "application/json",
    body: toJsonString(
      ListServerLogsResponseSchema,
      create(ListServerLogsResponseSchema, {
        entries,
        latestId,
        bufferSize: entries.length,
        truncated: false,
      }),
    ),
  });
}
