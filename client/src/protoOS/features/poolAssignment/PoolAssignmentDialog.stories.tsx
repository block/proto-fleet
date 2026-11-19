import { useEffect, useState } from "react";
import { action } from "storybook/actions";
import { removePoolAttempt, savePoolAttempt } from "./attemptStorage";
import PoolAssignmentDialog from "./PoolAssignmentDialog";
import type { PoolAssignmentAttempt, PoolAssignmentService } from "@/protoOS/api/adapters/poolAssignment";
import { newUUID } from "@/protoOS/utils/uuid";

const boards = Array.from({ length: 60 }, (_, index) => ({
  mac: `aa:bb:cc:00:00:${index.toString(16).padStart(2, "0")}`,
  location: `Tank 1 · Module ${Math.floor(index / 3) + 1} · Board ${(index % 3) + 1}`,
}));
const pools = [
  {
    id: "1",
    name: "Primary",
    url: "stratum+tcp://pool.example:3333",
    username: "account.region.worker",
    enabled: true,
    priority: 0,
  },
];
const saved = { id: "local", name: "Primary pools", revision: "1", mining: true, pools };
const capacity = { groupsUsed: 2, maxGroups: 289, maxBoards: 288, maxPools: 3 };
const workerNamesUnavailable = async (): Promise<never> => {
  throw new Error("Use a CT1 controller or simulator to update worker names.");
};
const createService = (): PoolAssignmentService => {
  const configurations = [saved];
  return {
    inspectWorkerNames: workerNamesUnavailable,
    reviewWorkerNames: workerNamesUnavailable,
    applyWorkerNames: workerNamesUnavailable,
    async listConfigurations() {
      return configurations.map((set) => ({
        ...set,
        assignedCount: 0,
        deletable: true,
        isDefault: false,
      }));
    },
    async listConfigurationPage(offset, limit) {
      return {
        configurations: configurations.slice(offset, offset + limit).map((set) => ({
          ...set,
          assignedCount: 0,
          deletable: true,
          isDefault: false,
        })),
        total: configurations.length,
        capacity,
      };
    },
    async deleteConfiguration(id) {
      configurations.splice(
        configurations.findIndex((set) => set.id === id),
        1,
      );
    },
    async inspect(target) {
      return {
        savedPools: configurations.map((set) => ({ ...set, projectedBoards: target.macs.length })),
        mixedConfigurations: false,
        capacity,
      };
    },
    async review(target, choice) {
      return {
        id: "story",
        name: choice.name || saved.name,
        configurationOnly: choice.createOnly === true,
        target,
        boards: boards.filter((board) => target.macs.includes(board.mac)),
        stamp: "story",
        pools: choice.pools,
        savedPoolSet: configurations.find((set) => set.id === choice.savedPoolSetId),
        usernameSource: choice.usernameSource,
        capacity: {
          ...capacity,
          groupsUsed: configurations.length + (choice.savedPoolSetId ? 1 : 2),
          destinationBoards: choice.createOnly ? 0 : target.macs.length,
        },
        mining: true,
        requiresActivation: false,
        destinationId: "local",
        destinationRef: "proto-os:pools:story",
        transfers: [{ source: "default", macs: target.macs }],
      };
    },
    async apply(plan, track) {
      const configuredPools =
        plan.savedPoolSet?.pools ??
        plan.pools.map((pool, priority) => ({
          id: String(priority + 1),
          name: pool.name,
          url: pool.url,
          username: pool.username,
          enabled: true,
          priority,
        }));
      const destinationId = plan.savedPoolSet?.id ?? newUUID();
      if (!plan.savedPoolSet)
        configurations.push({ ...saved, id: destinationId, name: plan.name, pools: configuredPools });
      track({
        configurationOnly: plan.configurationOnly,
        id: plan.id,
        macs: plan.target.macs,
        destinationId,
        destinationRef: "proto-os:pools:story",
        pools: configuredPools,
        configurationConfirmed: true,
        operationIds: [],
        phase: "Checking assignments",
      });
      return {
        state: "saved",
        message: "Pool assignment saved.",
        confirmed: plan.target.macs.length,
        total: plan.target.macs.length,
        blockers: [],
      };
    },
    async status(attempt) {
      return {
        state: "pending",
        message: "Waiting for the controller to finish moving the selected hashboards.",
        confirmed: 0,
        total: attempt.macs.length,
        blockers: [
          { mac: attempt.macs[0], reason: "Waiting for the previous owner to confirm that the board stopped." },
        ],
      };
    },
    async checkMiningRecovery(attempt, _controllerId, track) {
      const restored = { ...attempt, restoreLegacyMining: false };
      track(restored);
      return {
        state: "unknown",
        message: "Check saved changes.",
        confirmed: 0,
        total: restored.macs.length,
        blockers: [],
      };
    },
  };
};
const service = createService();
export const Configure = () => {
  const [service] = useState(createService);
  return (
    <PoolAssignmentDialog
      service={service}
      controllerId="storybook-pool-configure"
      target={{ macs: boards.map((board) => board.mac) }}
      onDismiss={action("dismiss pool assignment")}
    />
  );
};
export const Unavailable = () => (
  <PoolAssignmentDialog
    service={{
      ...service,
      inspect: async () => {
        throw new Error("The controller is unavailable. Reload the selection.");
      },
    }}
    controllerId="storybook-pool-unavailable"
    target={{ macs: boards.map((board) => board.mac) }}
    onDismiss={action("dismiss pool assignment")}
  />
);
export const Pending = () => {
  const [ready, setReady] = useState(false);
  useEffect(() => {
    const attempt: PoolAssignmentAttempt = {
      id: "story",
      macs: boards.slice(0, 3).map((board) => board.mac),
      destinationRef: "proto-os:pools:story",
      destinationId: "local",
      pools,
      configurationConfirmed: true,
      operationIds: ["transfer"],
      phase: "Waiting for selected hashboards to stop",
    };
    savePoolAttempt("storybook-pool-pending", attempt);
    const timer = setTimeout(() => setReady(true), 0);
    return () => {
      clearTimeout(timer);
      removePoolAttempt("storybook-pool-pending", "story");
    };
  }, []);
  return ready ? (
    <PoolAssignmentDialog
      service={service}
      controllerId="storybook-pool-pending"
      target={{ macs: boards.slice(0, 3).map((board) => board.mac) }}
      openStatus
      onDismiss={action("dismiss pool assignment")}
    />
  ) : null;
};
export default { title: "Proto OS/Modules/Pool assignments" };
