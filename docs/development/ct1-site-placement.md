# CT1 site placement

CT1 controllers are facility peers of buildings. `SiteService.AssignContainersToSite`
moves a whole container to a site, or to Unassigned when the target is omitted.
Tanks and modules follow their controller; they cannot be assigned independently.
The endpoint accepts at most 1,000 identifiers and applies the batch atomically.
Like building assignment, a move replaces the previous site; there is no rack
conflict or force option because containers cannot belong to racks.

The server locks the destination site before controller rows in ID order, then
checks `site:manage` at both the destination and every locked source placement.
Unassigned requires organization-wide authority. Cross-org, missing, and ordinary
miner identifiers reject the batch. The generic miner assignment endpoint rejects
containers. Moves share controller row locks with equipment command admission.
The activity log records `container.assigned_to_site` after a successful commit.

Operators can add unassigned containers and change existing container placement
in Manage Site, including the manage step immediately after creating a site.
Containers and conventional buildings share the Buildings list, count, preview,
and Manage buildings picker. The picker applies the same eligibility rules to
both: current-site and unassigned rows are selectable; other-site rows are disabled.
The Buildings list/grid container menus and the container detail menu also offer
Change site. These operations use the same API; unavailable controllers can move
without a live device connection.
The Change site action uses the server's current-site management capability,
including site-scoped grants. Saving rechecks both source and destination sites.

Membership modals use `ContainerService.ListContainerPlacements`, which returns
only container identity, name, site, and tank count. Each row requires `site:read`
and `site:manage` at its current placement (organization-wide for Unassigned).
Placement-only managers do not need `fleet:read` or `miner:read`. Container detail
and telemetry lists still require all three read permissions.

CLI operators use `fleetcli sites assign-containers --container-identifiers <id>
--target-site-id <site-id>`. Omit `--target-site-id` to unassign the container.
The command uses the same assignment API and permission checks as the UI.

Pairing has no site argument. Pairing a new container leaves it unassigned,
as the pairing API does for ordinary miners; sidebar scope never silently changes
placement. Assign its site explicitly after pairing. Deleting a site unassigns
its containers through the existing device cascade, preserving their inventory.

## Ownership and history

The controller's `device.site_id` remains the current-site source of truth.
`container_inventory.org_id` has a composite foreign key to `(device.id, org_id)`;
tank/module children retain their existing foreign-key chain to that root.
The `container_scope` view centralizes the root ownership join for child history
and placement reads. Container detail/equipment queries independently start from
the org-filtered controller. Database triggers also reject building/rack placement,
including device-class changes that would introduce invalid placement.

Inventory and site summary metrics follow the current site immediately. Site
summaries include CT1 module counts and current module measurements, matching the
unified Miners inventory: module power is PSU input, not container facility power.
Container and tank detail retain their distinct facility/PDU power measurements.
Site-selected schedules resolve the current device placement, including CT1s.
Before enqueueing, schedules skip devices that do not advertise the requested
command and report the capability skip in activity. CT1s currently support tank
and module controls, not generic sleep, reboot, or power-target commands, so those
schedule actions skip CT1s while continuing on supported miners. Site placement
does not add generic CT1 schedule or curtailment command support.

Moving a container does not rewrite the recorded site on poll-time observations
or hashboard installations. Module/tank history
requires access to the current container and filters samples by their recorded
site. An operator limited to the old site loses access to the moved container;
access to its new site alone does not grant its old site's samples.

Fleet/site dashboard rollups are an exception: their refresh deletes and rebuilds
metric buckets within its refresh window using the device's **current** site.
After a move, pre-move buckets rebuilt by a refresh or backfill can therefore be
attributed to the destination site. Buckets outside the rebuilt window keep their
existing attribution. This is the same rollup behavior used for conventional
miners, separate from the recorded-site filtering of module/tank history.

## CSV and migration

The site-map CSV format models buildings, conventional racks, and miners. CT1s
are deliberately excluded: mapping a controller into a miner or building row
would confuse its device identifier with building IDs and could imply invalid
rack membership. Manage CT1 placement through the container site controls.

Migration 1023 backfills root org ownership and installs placement constraints.
It refuses pre-existing live containers in buildings/racks instead of silently moving
them. Correct those placements before upgrading. Deleted controllers' retained
placements do not block the upgrade. The down migration removes the
new view, ownership column, and triggers; it preserves site assignments and all
inventory/history rows.

Before applying migration 1023, run this read-only preflight against the target
database. It reports every live CT1 with forbidden building/rack placement,
including the site to preserve:

```sql
SELECT d.org_id, d.id AS device_id, d.device_identifier, d.site_id, d.building_id,
       ARRAY(SELECT m.device_set_id FROM device_set_membership m
             WHERE m.device_id=d.id AND m.device_set_type='rack'
             ORDER BY m.device_set_id) AS rack_ids
FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id
WHERE dd.device_class=2 AND d.deleted_at IS NULL AND dd.deleted_at IS NULL
  AND (d.building_id IS NOT NULL OR EXISTS (
    SELECT 1 FROM device_set_membership m
    WHERE m.device_id=d.id AND m.device_set_type='rack'))
ORDER BY d.org_id, d.id;
```

For each result, verify the controller identity and intended site with the site
operator, and retain the query output for the change record. During a maintenance
window with placement/discovery writers stopped, remove only the invalid building
and rack associations. The following transaction is a template for **one reviewed
controller**; substitute its organization and device IDs in every predicate:

```sql
BEGIN;
SELECT id, device_identifier, site_id, building_id FROM device
WHERE org_id = <org_id> AND id = <device_id> FOR UPDATE;
DELETE FROM device_set_membership
WHERE org_id = <org_id> AND device_id = <device_id> AND device_set_type = 'rack';
UPDATE device SET building_id = NULL
WHERE org_id = <org_id> AND id = <device_id>;
SELECT id, device_identifier, site_id, building_id FROM device
WHERE org_id = <org_id> AND id = <device_id>;
COMMIT;
```

Rack-slot rows cascade from the removed rack membership. Keep group memberships,
`device.site_id`, and all inventory/history rows unchanged. Rerun the preflight;
upgrade only when it returns no rows. If the migration was already attempted and
marked dirty, keep the service stopped, back up the database, and have the
operator inspect both the schema and `schema_migrations`. Only after proving that
1023 rolled back may they restore the last successfully applied version and retry
the upgrade. Do not mark 1023 as applied or copy a recovery version from an
unrelated migration. The exception lists offending controllers and points here.

The placement guards take device `NO KEY UPDATE` locks, which remain compatible
with membership foreign-key `KEY SHARE` locks. Classification changes acquire
discovery then device locks. Building/rack placement never waits in the inverse
order: a conflicting discovery lock causes SQLSTATE `40001`, so the existing
placement transaction retry reruns against the committed classification. Unchanged
class refreshes do not lock devices. Neither race can commit a container inside a
building or rack.
