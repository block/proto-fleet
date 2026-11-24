import { Code } from "@connectrpc/connect";

export const isAuthenticationError = (error: unknown) => {
  const failure = error as { status?: number; error?: { code?: number } } | undefined;
  return failure?.status === 401 || failure?.error?.code === Code.Unauthenticated;
};
