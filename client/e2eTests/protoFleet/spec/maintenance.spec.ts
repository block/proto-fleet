import { expect, test } from "../fixtures/pageFixtures";
import { MaintenancePage } from "../pages/maintenance";

test("maintenance ticket completion consumes inventory exactly once", async ({ page }) => {
  const maintenance = new MaintenancePage(page);
  await maintenance.open();
  const miner = await maintenance.ensurePlacedMiner();
  const suffix = `${Date.now()}`;
  const partName = `E2E fan ${suffix}`;
  await maintenance.importPartThroughUI(
    `name,type,manufacturer,part_number,site_name,on_hand,reorder_point,bin_location\n${partName},Cooling,E2E,F-${suffix},${miner.site.name},3,1,E2E-A`,
    partName,
  );

  const parts = await maintenance.rpc<{ parts: Array<{ id: string; name: string; onHand: number }> }>(
    "inventory.v1.InventoryService/ListInventoryParts",
    { filter: { siteIds: [miner.site.id] }, pageSize: 100 },
  );
  const part = parts.body.parts.find((item) => item.name === partName);
  expect(part).toBeTruthy();

  const ticket = await maintenance.createMinerTicketThroughUI(miner.site.name, `E2E diagnosis ${suffix}`);
  await maintenance.assignStartAndCommentThroughUI(ticket, "E2E verified repair");
  await maintenance.completeTicketThroughUI(partName);

  const completion = {
    id: ticket.id,
    status: "TICKET_STATUS_COMPLETED",
    resolution: "TICKET_RESOLUTION_REPAIRED",
    repairLocation: "REPAIR_LOCATION_ON_RACK",
    partsSelection: { parts: [{ inventoryPartId: part!.id, partName, quantity: 1 }] },
  };
  // Retry the same terminal mutation through the API to prove completion is idempotent.
  expect((await maintenance.rpc("maintenance.v1.MaintenanceService/UpdateRepairTicket", completion)).status).toBe(200);

  const after = await maintenance.rpc<{ part: { onHand: number } }>("inventory.v1.InventoryService/GetInventoryPart", {
    id: part!.id,
  });
  expect(after.body.part.onHand).toBe(2);

  const history = await maintenance.rpc<{ tickets: Array<{ ticket?: { id: string } }> }>(
    "maintenance.v1.MaintenanceService/ListCompletedTickets",
    { pageSize: 100 },
  );
  expect(history.body.tickets.some((item) => item.ticket?.id === ticket.id)).toBe(true);

  const activity = await maintenance.rpc<{ activities: Array<{ eventType: string }> }>(
    "activity.v1.ActivityService/ListActivities",
    { pageSize: 100 },
  );
  const activityTypes = activity.body.activities.map((item) => item.eventType);
  expect(activityTypes).toContain("maintenance.ticket_updated");
  expect(activityTypes).toContain("inventory.parts_imported");

  await page.reload();
  await maintenance.openHistory();
  await maintenance.expectTicket(ticket.ticketNumber);
  await maintenance.openInventory();
  await expect(page.getByText(partName, { exact: true })).toBeVisible();
});
