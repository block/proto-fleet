import { type ReactNode, useCallback, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import {
  type BuildingCreateSeed,
  FleetCreateFlowContext,
  type FleetCreateFlowContextValue,
  type RackCreateSeed,
  type SiteCreateSeed,
} from "./context";
import { emptyBuildingFormValues, useBuildings } from "@/protoFleet/api/buildings";
import { type Building, BuildingWithCountsSchema } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type Site, type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { emptySiteFormValues, useSites } from "@/protoFleet/api/sites";
import BuildingModals from "@/protoFleet/features/buildings/components/BuildingModals";
import BuildingSettingsModal from "@/protoFleet/features/buildings/components/BuildingSettingsModal";
import { useBuildingModals } from "@/protoFleet/features/buildings/hooks/useBuildingModals";
import { ManageRackModal, type RackFormData } from "@/protoFleet/features/fleetManagement/components/ManageRackModal";
import RackSettingsModal from "@/protoFleet/features/fleetManagement/components/RackSettingsModal";
import SiteModals from "@/protoFleet/features/sites/components/SiteModals";
import SiteSettingsModal from "@/protoFleet/features/sites/components/SiteSettingsModal";
import { useSiteModals } from "@/protoFleet/features/sites/hooks/useSiteModals";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import { pushToast, STATUSES } from "@/shared/features/toaster";

// Hoisted create-flow host. Mounted once in FleetLayout so any fleet tab can
// launch a create flow in place — no cross-page navigation. Owns the rack +
// building create modal stacks; the site flow extends this provider.

// Matches the server-side max_items cap on AssignDevicesToSite/Building and
// AssignRacksTo* — mirrors MinerReparentPicker.MAX_REASSIGN_BATCH. Checked
// before creating the parent so an over-cap seed can't leave an orphaned
// empty parent when the assignment RPC rejects.
const MAX_REASSIGN_BATCH = 10000;

const overBatchCap = (seed: { buildingIds?: bigint[]; rackIds?: bigint[]; minerIds?: string[] }): boolean =>
  (seed.buildingIds?.length ?? 0) > MAX_REASSIGN_BATCH ||
  (seed.rackIds?.length ?? 0) > MAX_REASSIGN_BATCH ||
  (seed.minerIds?.length ?? 0) > MAX_REASSIGN_BATCH;

const FleetCreateFlowProvider = ({ children, sites }: { children: ReactNode; sites: SiteWithCounts[] }) => {
  const [entitiesChangedAt, setEntitiesChangedAt] = useState(0);
  const bumpEntities = useCallback(() => setEntitiesChangedAt(Date.now()), []);

  // Rack create flow. rackSettings drives RackSettingsModal; once the
  // operator continues, rackFormData opens ManageRackModal seeded with the
  // selected miners. rackSeed survives the settings step so the miners reach
  // the manage modal.
  const [rackSettingsOpen, setRackSettingsOpen] = useState(false);
  const [rackFormData, setRackFormData] = useState<RackFormData | null>(null);
  const [rackSeed, setRackSeed] = useState<RackCreateSeed | null>(null);
  // Holds a seed whose miners have a placement the new rack would clear,
  // until the operator confirms; null when no confirmation is pending.
  const [rackConflictSeed, setRackConflictSeed] = useState<RackCreateSeed | null>(null);

  const openRackSettings = useCallback((seed: RackCreateSeed) => {
    setRackSeed(seed);
    setRackFormData(null);
    setRackSettingsOpen(true);
  }, []);

  const launchCreateRack = useCallback(
    (seed: RackCreateSeed) => {
      if (seed.conflictCount && seed.conflictCount > 0) {
        setRackConflictSeed(seed);
        return;
      }
      openRackSettings(seed);
    },
    [openRackSettings],
  );

  const confirmRackConflict = useCallback(() => {
    if (rackConflictSeed) openRackSettings(rackConflictSeed);
    setRackConflictSeed(null);
  }, [rackConflictSeed, openRackSettings]);

  const closeRackFlow = useCallback(() => {
    setRackSettingsOpen(false);
    setRackFormData(null);
    setRackSeed(null);
  }, []);

  const handleRackSettingsContinue = useCallback((formData: RackFormData) => {
    setRackSettingsOpen(false);
    setRackFormData(formData);
  }, []);

  const handleRackSaved = useCallback(() => {
    bumpEntities();
    closeRackFlow();
  }, [bumpEntities, closeRackFlow]);

  // Building create flow. The settings step is hosted directly (not via the
  // hook) so we can intercept create-success to assign the seed and chain
  // into manage; the manage/edit/delete surfaces reuse a controller-owned
  // useBuildingModals instance rendered through <BuildingModals>.
  const { createBuilding, assignRacksToBuilding, assignDevicesToBuilding } = useBuildings();
  const buildingModals = useBuildingModals({ refetchBuildings: bumpEntities });
  const [buildingSeed, setBuildingSeed] = useState<BuildingCreateSeed | null>(null);
  // Holds a seed whose items have existing parents until the operator
  // confirms the move; null when no confirmation is pending.
  const [buildingConflictSeed, setBuildingConflictSeed] = useState<BuildingCreateSeed | null>(null);
  // In-flight guard for the provider-owned building create. The `saving` prop
  // lags a render behind the click (setState batching), so the ref is what
  // actually blocks a double-click from dispatching two CreateBuilding calls.
  const [creatingBuilding, setCreatingBuilding] = useState(false);
  const creatingBuildingRef = useRef(false);

  const launchCreateBuilding = useCallback((seed: BuildingCreateSeed) => {
    if (overBatchCap(seed)) {
      pushToast({
        message: `Can't add more than ${MAX_REASSIGN_BATCH} items at once. Filter the selection and try again.`,
        status: STATUSES.error,
      });
      return;
    }
    if (seed.conflictCount && seed.conflictCount > 0) {
      setBuildingConflictSeed(seed);
      return;
    }
    setBuildingSeed(seed);
  }, []);

  const confirmBuildingConflict = useCallback(() => {
    setBuildingSeed(buildingConflictSeed);
    setBuildingConflictSeed(null);
  }, [buildingConflictSeed]);

  const closeBuildingSettings = useCallback(() => setBuildingSeed(null), []);

  // create → assign seeded racks/miners (force-clearing prior memberships,
  // mirroring the reparent confirm path) → open manage for positioning.
  const handleBuildingCreate = useCallback(
    async (values: Parameters<typeof createBuilding>[0]["values"], siteId: bigint) => {
      // Synchronous re-entry guard: a double-click reaches here twice before
      // the `saving` prop re-renders, which would create duplicate buildings.
      if (creatingBuildingRef.current) return;
      creatingBuildingRef.current = true;
      setCreatingBuilding(true);
      try {
        const seed = buildingSeed;
        const building = await new Promise<Building | null>((resolve) => {
          void createBuilding({
            values,
            siteId,
            onSuccess: (b) => resolve(b),
            onError: (msg) => {
              pushToast({ message: `Failed to create building: ${msg}`, status: STATUSES.error });
              resolve(null);
            },
          });
        });
        if (!building) return;

        if (seed && seed.rackIds.length > 0) {
          await new Promise<void>((resolve) => {
            void assignRacksToBuilding({
              racks: seed.rackIds.map((rackId) => ({ rackId })),
              targetBuildingId: building.id,
              onSuccess: () => resolve(),
              onError: (msg) => {
                pushToast({ message: `Building created, but adding racks failed: ${msg}`, status: STATUSES.error });
                resolve();
              },
            });
          });
        }
        if (seed && seed.minerIds.length > 0) {
          await new Promise<void>((resolve) => {
            void assignDevicesToBuilding({
              targetBuildingId: building.id,
              deviceIdentifiers: seed.minerIds,
              forceClearConflictingRackMembership: true,
              onSuccess: () => resolve(),
              onError: (msg) => {
                pushToast({ message: `Building created, but adding miners failed: ${msg}`, status: STATUSES.error });
                resolve();
              },
            });
          });
        }

        bumpEntities();
        setBuildingSeed(null);
        const siteName = sites.find((s) => s.site?.id === siteId)?.site?.name;
        buildingModals.openManage(
          create(BuildingWithCountsSchema, { building, rackCount: BigInt(seed?.rackIds.length ?? 0) }),
          siteName,
          seed?.minerIds.length || undefined,
        );
      } finally {
        creatingBuildingRef.current = false;
        setCreatingBuilding(false);
      }
    },
    [buildingSeed, createBuilding, assignRacksToBuilding, assignDevicesToBuilding, bumpEntities, sites, buildingModals],
  );

  // Site create flow. Like building, the settings step is hosted directly so
  // we can intercept continue → create → assign → manage. The manage step
  // routes through edit mode (openManageEdit) because create mode gates
  // building assignment until the site exists.
  const { createSite, assignBuildingsToSite, assignRacksToSite, assignDevicesToSite } = useSites();
  const siteModals = useSiteModals({ refetchSites: bumpEntities });
  const [siteSeed, setSiteSeed] = useState<SiteCreateSeed | null>(null);
  const [siteConflictSeed, setSiteConflictSeed] = useState<SiteCreateSeed | null>(null);
  const [creatingSite, setCreatingSite] = useState(false);
  const creatingSiteRef = useRef(false);

  const launchCreateSite = useCallback((seed: SiteCreateSeed) => {
    if (overBatchCap(seed)) {
      pushToast({
        message: `Can't add more than ${MAX_REASSIGN_BATCH} items at once. Filter the selection and try again.`,
        status: STATUSES.error,
      });
      return;
    }
    if (seed.conflictCount && seed.conflictCount > 0) {
      setSiteConflictSeed(seed);
      return;
    }
    setSiteSeed(seed);
  }, []);

  const confirmSiteConflict = useCallback(() => {
    setSiteSeed(siteConflictSeed);
    setSiteConflictSeed(null);
  }, [siteConflictSeed]);

  const closeSiteSettings = useCallback(() => setSiteSeed(null), []);

  const handleSiteCreate = useCallback(
    async (values: Parameters<typeof createSite>[0]["values"]) => {
      // Synchronous re-entry guard against duplicate CreateSite on double-click.
      if (creatingSiteRef.current) return;
      creatingSiteRef.current = true;
      setCreatingSite(true);
      try {
        const seed = siteSeed;
        const site = await new Promise<Site | null>((resolve) => {
          void createSite({
            values,
            onSuccess: (s) => resolve(s),
            onError: (msg) => {
              pushToast({ message: `Failed to create site: ${msg}`, status: STATUSES.error });
              resolve(null);
            },
          });
        });
        if (!site) return;

        if (seed && seed.buildingIds.length > 0) {
          await new Promise<void>((resolve) => {
            void assignBuildingsToSite({
              buildingIds: seed.buildingIds,
              targetSiteId: site.id,
              onSuccess: () => resolve(),
              onError: (msg) => {
                pushToast({ message: `Site created, but adding buildings failed: ${msg}`, status: STATUSES.error });
                resolve();
              },
            });
          });
        }
        if (seed && seed.rackIds.length > 0) {
          await new Promise<void>((resolve) => {
            void assignRacksToSite({
              rackIds: seed.rackIds,
              targetSiteId: site.id,
              onSuccess: () => resolve(),
              onError: (msg) => {
                pushToast({ message: `Site created, but adding racks failed: ${msg}`, status: STATUSES.error });
                resolve();
              },
            });
          });
        }
        if (seed && seed.minerIds.length > 0) {
          await new Promise<void>((resolve) => {
            void assignDevicesToSite({
              targetSiteId: site.id,
              deviceIdentifiers: seed.minerIds,
              forceClearConflictingRackMembership: true,
              onSuccess: () => resolve(),
              onError: (msg) => {
                pushToast({ message: `Site created, but adding miners failed: ${msg}`, status: STATUSES.error });
                resolve();
              },
            });
          });
        }

        bumpEntities();
        setSiteSeed(null);
        siteModals.openManageEdit(site, {
          unassignedRackCount: seed?.rackIds.length || undefined,
          unassignedMinerCount: seed?.minerIds.length || undefined,
        });
      } finally {
        creatingSiteRef.current = false;
        setCreatingSite(false);
      }
    },
    [siteSeed, createSite, assignBuildingsToSite, assignRacksToSite, assignDevicesToSite, bumpEntities, siteModals],
  );

  const value = useMemo<FleetCreateFlowContextValue>(
    () => ({ launchCreateRack, launchCreateBuilding, launchCreateSite, entitiesChangedAt }),
    [launchCreateRack, launchCreateBuilding, launchCreateSite, entitiesChangedAt],
  );

  return (
    <FleetCreateFlowContext.Provider value={value}>
      {children}
      {rackSettingsOpen ? (
        <RackSettingsModal
          show={rackSettingsOpen}
          existingRacks={[]}
          onDismiss={closeRackFlow}
          onContinue={handleRackSettingsContinue}
        />
      ) : null}
      {rackFormData ? (
        <ManageRackModal
          show={!!rackFormData}
          rackSettings={rackFormData}
          existingRacks={[]}
          seededMinerIds={rackSeed?.minerIds}
          onDismiss={closeRackFlow}
          onSave={handleRackSaved}
        />
      ) : null}
      {rackConflictSeed ? (
        <Dialog
          open
          title="Move selected miners to a new rack?"
          subtitle={`${rackConflictSeed.conflictCount} of the selected miners are currently in a site, building, or rack. A new rack has no site, so continuing will clear those placements.`}
          onDismiss={() => setRackConflictSeed(null)}
          buttons={[
            { text: "Cancel", variant: variants.secondary, onClick: () => setRackConflictSeed(null) },
            { text: "Continue", variant: variants.primary, onClick: confirmRackConflict },
          ]}
        />
      ) : null}
      {buildingSeed ? (
        <BuildingSettingsModal
          open
          mode="create"
          initialValues={emptyBuildingFormValues()}
          sites={sites}
          onSave={handleBuildingCreate}
          onDismiss={closeBuildingSettings}
          saving={creatingBuilding || buildingModals.saving}
        />
      ) : null}
      <BuildingModals modals={buildingModals} sites={sites} />
      {buildingConflictSeed ? (
        <Dialog
          open
          title="Move selected items to a new building?"
          subtitle={`${buildingConflictSeed.conflictCount} of the selected items already belong to another building or site. Continuing will move them into the new building.`}
          onDismiss={() => setBuildingConflictSeed(null)}
          buttons={[
            { text: "Cancel", variant: variants.secondary, onClick: () => setBuildingConflictSeed(null) },
            { text: "Continue", variant: variants.primary, onClick: confirmBuildingConflict },
          ]}
        />
      ) : null}
      {siteSeed ? (
        <SiteSettingsModal
          open
          mode="create"
          initialValues={emptySiteFormValues()}
          onContinue={(values) => void handleSiteCreate(values)}
          onDismiss={closeSiteSettings}
          saving={creatingSite || siteModals.saving}
        />
      ) : null}
      <SiteModals modals={siteModals} sites={sites} />
      {siteConflictSeed ? (
        <Dialog
          open
          title="Move selected items to a new site?"
          subtitle={`${siteConflictSeed.conflictCount} of the selected items already belong to another site. Continuing will move them into the new site.`}
          onDismiss={() => setSiteConflictSeed(null)}
          buttons={[
            { text: "Cancel", variant: variants.secondary, onClick: () => setSiteConflictSeed(null) },
            { text: "Continue", variant: variants.primary, onClick: confirmSiteConflict },
          ]}
        />
      ) : null}
    </FleetCreateFlowContext.Provider>
  );
};

export default FleetCreateFlowProvider;
