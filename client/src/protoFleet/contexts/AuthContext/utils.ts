import { AuthTokens } from "@/protoFleet/contexts/AuthContext/AuthContext";

export const getAuthHeader = (authTokens: AuthTokens) => ({
  headers: { Authorization: `Bearer ${authTokens.accessToken.value}` },
});
