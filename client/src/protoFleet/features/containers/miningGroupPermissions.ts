// Mining-group workflows may stop and start boards while changing configuration.
// Ordinary miner pool updates retain their separate permission policy.
export function hasMiningGroupPermissions(permissions: readonly string[], operation: "pools" | "activate") {
  return [
    operation === "activate" ? "miner:activate_mining_groups" : "miner:update_pools",
    "miner:start_mining",
    "miner:stop_mining",
  ].every((permission) => permissions.includes(permission));
}
