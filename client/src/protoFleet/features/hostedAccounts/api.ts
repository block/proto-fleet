import { Code, ConnectError, createClient, type Interceptor } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import { API_PROXY_BASE } from "@/protoFleet/api/constants";
import { HostedAccountService } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { transport } from "@/protoFleet/api/transport";
import { observabilityInterceptors } from "@/shared/observability";

// Bound each read separately so slow multi-page loads can finish while stalled
// requests release their polling slot. Streaming CSV exports have no deadline.
export const ACCOUNT_READ_TIMEOUT_MS = 30_000;

export const hostedAccountsClient = createClient(HostedAccountService, transport);

// A page owns its client. No global mutable account selection can retarget a
// queued mutation, another browser tab, or a host fleet operation.
export const createAccountClient = (
  accountId: string,
  lifetime: AbortSignal | (() => AbortSignal),
  { readOnly = false }: { readOnly?: boolean } = {},
) => {
  const scope: Interceptor = (next) => (request) => {
    const signal = typeof lifetime === "function" ? lifetime() : lifetime;
    if (signal.aborted || request.signal.aborted)
      throw new ConnectError("Account workspace is no longer active", Code.Canceled);
    if (readOnly && !/^(Get|List)/.test(request.method.name) && request.method.name !== "ExportAccountActivities")
      throw new ConnectError("Actions are unavailable in this read-only preview.", Code.PermissionDenied);
    request.header.set("X-Fleet-Account-ID", accountId);
    return next({
      ...request,
      signal: AbortSignal.any([request.signal, signal]),
    });
  };
  return createClient(
    HostedAccountService,
    createConnectTransport({
      baseUrl: `${API_PROXY_BASE}/`,
      fetch: (input, init) => fetch(input, { ...init, credentials: "include" }),
      interceptors: [...observabilityInterceptors(), scope],
    }),
  );
};

// Effect replay can reopen the same mounted scope in StrictMode; a different
// account or login always gets a different connection object.
export const createAccountConnection = (accountId: string, options: { readOnly?: boolean } = {}) => {
  let controller = new AbortController();
  return {
    client: createAccountClient(accountId, () => controller.signal, options),
    get signal() {
      return controller.signal;
    },
    activate() {
      if (controller.signal.aborted) controller = new AbortController();
    },
    dispose() {
      controller.abort();
    },
  };
};

export const isRetryableReadError = (error: unknown) =>
  [Code.Unavailable, Code.DeadlineExceeded, Code.Internal, Code.Unknown, Code.ResourceExhausted].includes(
    ConnectError.from(error).code,
  );
