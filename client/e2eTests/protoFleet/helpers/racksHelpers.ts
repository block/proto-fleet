import { expect } from "@playwright/test";
import { type RackSelectorMiner, type RacksPage } from "../pages/racks";
import { PROTO_RIG_MODEL } from "./minerModels";

export function createZoneName(prefix: "A" | "B") {
  const suffix = Math.random()
    .toString(36)
    .replace(/[^a-z]+/g, "")
    .slice(0, 6);
  return `${prefix}-${suffix || "zone"}`;
}

export async function addSelectableMinersToSlots(
  racksPage: RacksPage,
  minerCount: number,
  slotNumbers: readonly number[],
): Promise<RackSelectorMiner[]> {
  expect(slotNumbers).toHaveLength(minerCount);

  await racksPage.clickAddMiners();
  await racksPage.waitForMinerSelectorListToLoad();

  const selectableMinerIndexes = await racksPage.getSelectableMinerIndexes(minerCount);
  const selectedMiners = await racksPage.getMinersFromSelector(selectableMinerIndexes);
  await racksPage.selectMinersInSelectorByIndex(selectableMinerIndexes);
  await racksPage.clickContinueInMinerSelector();

  for (let i = 0; i < selectedMiners.length; i++) {
    await racksPage.selectRackMiner(selectedMiners[i].ipAddress);
    await racksPage.clickRackSlot(slotNumbers[i]);
  }

  return selectedMiners;
}

export async function addSelectableRigMinersToSlots(
  racksPage: RacksPage,
  minerCount: number,
  slotNumbers: readonly number[],
): Promise<RackSelectorMiner[]> {
  expect(slotNumbers).toHaveLength(minerCount);

  await racksPage.clickAddMiners();
  await racksPage.waitForMinerSelectorListToLoad();
  await racksPage.filterModalType(PROTO_RIG_MODEL);
  await racksPage.waitForMinerSelectorListToLoad();

  const selectableMinerIndexes = await racksPage.getSelectableMinerIndexes(minerCount);
  const selectedMiners = await racksPage.getMinersFromSelector(selectableMinerIndexes);
  await racksPage.selectMinersInSelectorByIndex(selectableMinerIndexes);
  await racksPage.clickContinueInMinerSelector();

  for (let i = 0; i < selectedMiners.length; i++) {
    await racksPage.selectRackMiner(selectedMiners[i].ipAddress);
    await racksPage.clickRackSlot(slotNumbers[i]);
  }

  return selectedMiners;
}

export async function expectGridRackLabels(racksPage: RacksPage, expectedLabels: string[]) {
  await expect.poll(async () => await racksPage.getGridRackLabels()).toEqual(expectedLabels);
}

export async function expectListRackLabels(racksPage: RacksPage, expectedLabels: string[]) {
  await expect.poll(async () => await racksPage.listRackNames()).toEqual(expectedLabels);
}
