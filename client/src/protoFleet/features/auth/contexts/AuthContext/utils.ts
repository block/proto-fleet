import { AuthTokens } from "@/protoFleet/features/auth/contexts/AuthContext";

export const getAuthHeader = (authTokens: AuthTokens) => ({
  headers: { Authorization: `Bearer ${authTokens.accessToken.value}` },
});
