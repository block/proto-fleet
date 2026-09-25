import { useLayoutEffect, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { action } from "storybook/actions";

import RackSettingsModal from "./RackSettingsModal";
import { buildingsClient, deviceSetClient } from "@/protoFleet/api/clients";
import { ListBuildingsResponseSchema } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import {
  ListRackTypesResponseSchema,
  ListRackZonesResponseSchema,
} from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { SitesContext, type SitesContextValue } from "@/protoFleet/api/SitesContext";
import { useFleetStore } from "@/protoFleet/store";
import { createRefCountedStoryMock } from "@/shared/stories/createRefCountedStoryMock";

const storySitesContext: SitesContextValue = {
  sites: [create(SiteWithCountsSchema, { site: { id: 1n, name: "Austin", slug: "austin" } })],
  sitesError: null,
  sitesLoaded: true,
  sitesSettled: true,
  sitesPermissionDenied: false,
  siteCatalogAccessGranted: true,
  refetchSites: () => {},
  registerSitesPoll: () => () => {},
};

// Keep the form usable without a running backend, and restore each catalog
// method and permission when the last story instance unmounts.
const installStoryFixtures = createRefCountedStoryMock(() => {
  const originalListRackZones = deviceSetClient.listRackZones;
  const originalListRackTypes = deviceSetClient.listRackTypes;
  const originalListBuildings = buildingsClient.listBuildings;
  const originalPermissions = useFleetStore.getState().auth.permissions;

  deviceSetClient.listRackZones = async () =>
    create(ListRackZonesResponseSchema, { zones: ["North hall", "South hall"] });
  deviceSetClient.listRackTypes = async () =>
    create(ListRackTypesResponseSchema, { rackTypes: [{ rows: 4, columns: 3, rackCount: 6 }] });
  buildingsClient.listBuildings = async () =>
    create(ListBuildingsResponseSchema, {
      buildings: [{ building: { id: 11n, siteId: 1n, name: "Building A" } }],
    });
  useFleetStore.setState((state) => {
    state.auth.permissions = ["site:read", "site:manage"];
  });

  return () => {
    deviceSetClient.listRackZones = originalListRackZones;
    deviceSetClient.listRackTypes = originalListRackTypes;
    buildingsClient.listBuildings = originalListBuildings;
    useFleetStore.setState((state) => {
      state.auth.permissions = originalPermissions;
    });
  };
});

export default {
  title: "Proto Fleet/Rack Management/RackSettingsModal",
  component: RackSettingsModal,
};

export const CreateNew = () => {
  const [show, setShow] = useState(true);
  useLayoutEffect(installStoryFixtures, []);

  return (
    <SitesContext.Provider value={storySitesContext}>
      {!show ? (
        <div className="flex h-screen items-center justify-center">
          <button onClick={() => setShow(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
            Show Modal
          </button>
        </div>
      ) : null}
      <RackSettingsModal
        show={show}
        existingRacks={[]}
        onDismiss={() => {
          action("onDismiss")();
          setShow(false);
        }}
        onSubmit={(formData) => {
          action("onSubmit")(formData);
          setShow(false);
        }}
      />
    </SitesContext.Provider>
  );
};
