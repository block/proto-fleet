import { useFleetStore } from "@/protoFleet/store/useFleetStore";

export function seedMockAuth() {
  const state = useFleetStore.getState();
  state.auth.setIsAuthenticated(true);
  state.auth.setSessionExpiry(new Date(Date.now() + 86_400_000));
  state.auth.setUsername("demo");
  state.auth.setRole("SUPER_ADMIN");
  state.auth.setPermissions([
    "activity:read",
    "apikey:manage",
    "building:manage",
    "building:read",
    "curtailment:manage",
    "curtailment:read",
    "device:manage",
    "device:read",
    "miner:manage",
    "miner:read",
    "pool:manage",
    "pool:read",
    "rack:manage",
    "rack:read",
    "role:manage",
    "schedule:manage",
    "serverlog:read",
    "site:manage",
    "site:read",
    "user:read",
  ]);
  state.auth.setAuthLoading(false);

  console.warn("[mock] Auth seeded — running with fixture data from fleet-vision prototype");
}
