// Mining-group workflows may stop and start boards while changing configuration.
// Ordinary miner pool updates retain their separate permission policy.
const operationPermissions = {
  pools: "miner:update_pools",
  activate: "miner:activate_mining_groups",
  deactivate: "miner:deactivate_mining_groups",
};

export function hasMiningGroupPermissions(
  permissions: readonly string[],
  operation: "pools" | "activate" | "deactivate",
) {
  return [operationPermissions[operation], "miner:start_mining", "miner:stop_mining"].every((permission) =>
    permissions.includes(permission),
  );
}
