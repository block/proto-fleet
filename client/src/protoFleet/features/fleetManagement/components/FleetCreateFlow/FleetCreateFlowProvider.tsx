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
import { useHasPermission } from "@/protoFleet/store";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import { pushToast, STATUSES } from "@/shared/features/toaster";

// Hoisted create-flow host. Mounted once in FleetLayout so any fleet tab can
// launch a create flow in place — no cross-page navigation. Owns the rack +
// building create modal stacks; the site flow extends this provider.

// Server-side max_items caps differ by assignment RPC: AssignBuildingsToSite /
// AssignRacksToSite / AssignRacksToBuilding cap at 1000, while
// AssignDevicesToSite/Building (miners) cap at 10000. Checked before creating
// the parent so an over-cap seed can't leave an orphaned empty parent when the
// assignment RPC rejects.
const MAX_PARENT_BATCH = 1000;
const MAX_DEVICE_BATCH = 10000;

// Absolute upper bound on a rack's miner capacity: 12×12, the largest
// layout RackSettingsModal allows (rows/columns each clamp 1–12, matching
// the server's maxRackDimension). The rack create flow seeds the new rack
// with the operator's current miner selection, which in all-selection mode
// resolves to the full fleet (capped only at MAX_DEVICE_BATCH). Without a
// gate here, an oversized seed renders one MinersPane row per miner and
// freezes the browser long before the save-time capacity guard fires. Cap
// at the absolute max before ManageRackModal mounts — the exact
// chosen-capacity check still runs at save once rows×columns are picked.
const MAX_RACK_CAPACITY = 12 * 12;

// A seed that moves racked miners into a new building/site sends
// forceClearConflictingRackMembership, which the server gates on rack:manage.
// "Add to building/site" is reachable with only site:manage, so without this
// preflight a site-only operator would create the parent, then 403 on the
// assignment — leaving an orphaned empty parent.
const RACK_CLEAR_PERMISSION_MESSAGE =
  "You need rack permissions to move these miners out of their racks. Remove the racked miners from the selection or ask an admin.";

// Returns an operator-facing message when a seed exceeds the relevant cap, else
// null. Per type so the message names the right limit and entity.
const batchCapError = (seed: { buildingIds?: bigint[]; rackIds?: bigint[]; minerIds?: string[] }): string | null => {
  if ((seed.buildingIds?.length ?? 0) > MAX_PARENT_BATCH)
    return `Can't add more than ${MAX_PARENT_BATCH} buildings at once. Filter the selection and try again.`;
  if ((seed.rackIds?.length ?? 0) > MAX_PARENT_BATCH)
    return `Can't add more than ${MAX_PARENT_BATCH} racks at once. Filter the selection and try again.`;
  if ((seed.minerIds?.length ?? 0) > MAX_DEVICE_BATCH)
    return `Can't add more than ${MAX_DEVICE_BATCH} miners at once. Filter the selection and try again.`;
  return null;
};

const FleetCreateFlowProvider = ({
  children,
  sites,
  refetchSites,
  notifyMinersChanged,
}: {
  children: ReactNode;
  sites: SiteWithCounts[];
  // FleetLayout's site-catalog refetch. Called after a site is created so the
  // shared `sites` list (site column names, pickers) reflects the new site
  // immediately, not just on the next poll.
  refetchSites: () => void;
  // FleetLayout's Miners-tab refresh pulse. The Miners tab listens to
  // minersChangedAt (not entitiesChangedAt), so a create flow launched from
  // it must signal here for the moved miners' placement to refresh.
  notifyMinersChanged: () => void;
}) => {
  const [entitiesChangedAt, setEntitiesChangedAt] = useState(0);
  // Bumping entities also refreshes the Miners tab: every create flow can move
  // miners (rack seeds them; building/site direct-assign them), and the Miners
  // tab refreshes off minersChangedAt rather than entitiesChangedAt.
  const bumpEntities = useCallback(() => {
    setEntitiesChangedAt(Date.now());
    notifyMinersChanged();
  }, [notifyMinersChanged]);
  // Refresh the shared site catalog AND pulse list pages — used by the site
  // create/edit paths so the new site resolves everywhere right away. Also
  // bumps the header SitePicker's refresh signal so a site created via the
  // bulk "New site" seeded flow shows up there without a reload.
  const bumpSitesRevision = useFleetStore((state) => state.ui.bumpSitesRevision);
  const refreshSitesAndBump = useCallback(() => {
    refetchSites();
    bumpEntities();
    bumpSitesRevision();
  }, [refetchSites, bumpEntities, bumpSitesRevision]);

  // Prepopulate the new-rack Site dropdown from the page-header scope when a
  // single site is selected (undefined for "All sites" / "Unassigned").
  const activeSite = useFleetStore((state) => state.ui.activeSite);
  const scopedSiteId = useMemo(() => (activeSite.kind === "site" ? BigInt(activeSite.id) : undefined), [activeSite]);

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
      if ((seed.minerIds?.length ?? 0) > MAX_RACK_CAPACITY) {
        pushToast({
          message: `A rack holds at most ${MAX_RACK_CAPACITY} miners. Filter the selection and try again.`,
          status: STATUSES.error,
        });
        return;
      }
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
  // Gates the force-clear preflight below: a force-clearing device assignment
  // needs rack:manage on the server, but the create flow is reachable with
  // only site:manage.
  const canManageRacks = useHasPermission("rack:manage");
  const [buildingSeed, setBuildingSeed] = useState<BuildingCreateSeed | null>(null);
  // Holds a seed whose items have existing parents until the operator
  // confirms the move; null when no confirmation is pending.
  const [buildingConflictSeed, setBuildingConflictSeed] = useState<BuildingCreateSeed | null>(null);
  // In-flight guard for the provider-owned building create. The `saving` prop
  // lags a render behind the click (setState batching), so the ref is what
  // actually blocks a double-click from dispatching two CreateBuilding calls.
  const [creatingBuilding, setCreatingBuilding] = useState(false);
  const creatingBuildingRef = useRef(false);

  const launchCreateBuilding = useCallback(
    (seed: BuildingCreateSeed) => {
      const capErr = batchCapError(seed);
      if (capErr) {
        pushToast({ message: capErr, status: STATUSES.error });
        return;
      }
      // Block before creating the parent: a force-clear assignment would 403
      // for a site-only operator and strand an empty building.
      if (seed.forceClearRackMembership && !canManageRacks) {
        pushToast({ message: RACK_CLEAR_PERMISSION_MESSAGE, status: STATUSES.error });
        return;
      }
      if (seed.conflictCount && seed.conflictCount > 0) {
        setBuildingConflictSeed(seed);
        return;
      }
      setBuildingSeed(seed);
    },
    [canManageRacks],
  );

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

      // Preflight seeded racks against the chosen layout BEFORE creating the
      // building. The server's AssignRacksToBuilding now rejects an
      // over-capacity assign; without this guard the building is created and
      // then stranded with its racks unassigned (the assign failure only
      // toasts). Net-new == every seeded rack since the building is brand
      // new. Skipped at capacity 0 (unconfigured layout) — staging is allowed.
      const rackCapacity = values.aisles * values.racksPerAisle;
      if (buildingSeed && rackCapacity > 0 && buildingSeed.rackIds.length > rackCapacity) {
        pushToast({
          message: `This ${values.aisles}×${values.racksPerAisle} layout holds ${rackCapacity} rack(s), but ${buildingSeed.rackIds.length} are selected. Reduce the selection or increase the layout.`,
          status: STATUSES.error,
        });
        return;
      }

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
              forceClearConflictingRackMembership: seed.forceClearRackMembership ?? false,
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
  const siteModals = useSiteModals({ refetchSites: refreshSitesAndBump });
  const [siteSeed, setSiteSeed] = useState<SiteCreateSeed | null>(null);
  const [siteConflictSeed, setSiteConflictSeed] = useState<SiteCreateSeed | null>(null);
  const [creatingSite, setCreatingSite] = useState(false);
  const creatingSiteRef = useRef(false);

  const launchCreateSite = useCallback(
    (seed: SiteCreateSeed) => {
      const capErr = batchCapError(seed);
      if (capErr) {
        pushToast({ message: capErr, status: STATUSES.error });
        return;
      }
      // Block before creating the parent: a force-clear assignment would 403
      // for a site-only operator and strand an empty site.
      if (seed.forceClearRackMembership && !canManageRacks) {
        pushToast({ message: RACK_CLEAR_PERMISSION_MESSAGE, status: STATUSES.error });
        return;
      }
      if (seed.conflictCount && seed.conflictCount > 0) {
        setSiteConflictSeed(seed);
        return;
      }
      setSiteSeed(seed);
    },
    [canManageRacks],
  );

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
              forceClearConflictingRackMembership: seed.forceClearRackMembership ?? false,
              onSuccess: () => resolve(),
              onError: (msg) => {
                pushToast({ message: `Site created, but adding miners failed: ${msg}`, status: STATUSES.error });
                resolve();
              },
            });
          });
        }

        refreshSitesAndBump();
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
    [
      siteSeed,
      createSite,
      assignBuildingsToSite,
      assignRacksToSite,
      assignDevicesToSite,
      refreshSitesAndBump,
      siteModals,
    ],
  );

  const value = useMemo<FleetCreateFlowContextValue>(
    () => ({
      launchCreateRack,
      launchCreateBuilding,
      launchCreateSite,
      entitiesChangedAt,
      refreshEntities: refreshSitesAndBump,
    }),
    [launchCreateRack, launchCreateBuilding, launchCreateSite, entitiesChangedAt, refreshSitesAndBump],
  );

  return (
    <FleetCreateFlowContext.Provider value={value}>
      {children}
      {rackSettingsOpen ? (
        <RackSettingsModal
          show={rackSettingsOpen}
          existingRacks={[]}
          defaultSiteId={scopedSiteId}
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
          scopedSiteId={scopedSiteId}
          onDismiss={closeRackFlow}
          onSave={handleRackSaved}
        />
      ) : null}
      {rackConflictSeed ? (
        <Dialog
          open
          title="Move selected miners to a new rack?"
          subtitle={`${rackConflictSeed.conflictCount} of the selected miners are already in a rack, building, or site. Creating this rack will move them into it and clear their previous placement.`}
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
          // Pre-fill (and lock to) the page-header site scope.
          initialSiteId={scopedSiteId}
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
