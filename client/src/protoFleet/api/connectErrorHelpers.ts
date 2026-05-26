import { Code, ConnectError } from "@connectrpc/connect";

export function isUnimplementedConnectError(error: unknown): boolean {
  return ConnectError.from(error).code === Code.Unimplemented;
}
