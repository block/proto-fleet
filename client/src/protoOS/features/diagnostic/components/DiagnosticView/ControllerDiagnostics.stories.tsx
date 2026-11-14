import { useMemo } from "react";

import ControllerDiagnostics from "./ControllerDiagnostics";
import { MdkV2Adapter, type MdkV2Client } from "@/protoOS/api/adapters/mdkV2";
import MinerHostingContext from "@/protoOS/contexts/MinerHostingContext/MinerHostingContext";

type Scenario = "healthy" | "peerUnreachable" | "haUnsupported" | "refreshFails";

const iso = (ageMs: number) => new Date(Date.now() - ageMs).toISOString();

const fresh = (ageMs = 2_000) => ({
  sourceServerId: "ct1-a",
  observedAt: iso(ageMs),
  lastSuccessfulAt: iso(ageMs),
  freshness: "HA_OBSERVATION_FRESHNESS_FRESH",
});

// Wire JSON as the controller sends it; the generated types are private to the adapter.
const systemStatus = () => ({
  status: {
    serverId: "ct1-a",
    version: "2.0.0",
    haRole: "HA_ROLE_PRIMARY",
    containerWritePrimary: true,
    databaseConnected: true,
    dcsHealthy: true,
    haRecovery: {
      state: "HA_RECOVERY_STATE_STEADY",
      phase: "HA_RECOVERY_PHASE_STEADY",
    },
  },
});

const haStatus = (peerUnreachable: boolean) => ({
  status: {
    enabled: true,
    reportingServerId: "ct1-a",
    peerServerId: "ct1-b",
    clusterId: "ct1-cluster-01",
    identityState: "HA_IDENTITY_STATE_CONFIRMED",
    inventoryReady: true,
    observedAt: iso(1_000),
    members: [
      {
        id: "ct1-a",
        kind: "HA_MEMBER_ROLE_FRIGATE",
        local: true,
        hostname: "ct1-control-a",
        haRole: "HA_ROLE_PRIMARY",
        containerWritePrimary: true,
        roleObservation: fresh(),
        frigateReachable: true,
        frigateObservation: fresh(),
        recovery: { state: "HA_RECOVERY_STATE_STEADY" },
      },
      {
        id: "ct1-b",
        kind: "HA_MEMBER_ROLE_FRIGATE",
        hostname: "ct1-control-b",
        haRole: "HA_ROLE_STANDBY",
        containerWritePrimary: false,
        ...(peerUnreachable
          ? {
              roleObservation: {
                ...fresh(95_000),
                freshness: "HA_OBSERVATION_FRESHNESS_STALE",
              },
              frigateReachable: false,
              frigateObservation: {
                sourceServerId: "ct1-a",
                observedAt: iso(2_000),
                lastSuccessfulAt: iso(95_000),
                freshness: "HA_OBSERVATION_FRESHNESS_STALE",
                detail: "peer did not answer the latest probe",
              },
            }
          : {
              roleObservation: fresh(),
              frigateReachable: true,
              frigateObservation: fresh(),
              recovery: { state: "HA_RECOVERY_STATE_STEADY" },
            }),
      },
    ],
  },
});

const unimplemented = () => Promise.reject(Object.assign(new Error("unimplemented"), { status: 501 }));
const offline = () => Promise.reject(Object.assign(new Error("controller offline"), { status: 503 }));

const ControllersPreview = ({ scenario }: { scenario: Scenario }) => {
  const adapter = useMemo(() => {
    let polls = 0;
    // The first poll succeeds and every later one fails, so the alert appears over the retained result.
    const refreshFailed = () => scenario === "refreshFails" && polls > 1;
    const client: Partial<MdkV2Client> = {
      systemServiceGetHaStatus: (() => {
        polls += 1;
        if (scenario === "haUnsupported") return unimplemented();
        if (refreshFailed()) return offline();
        return Promise.resolve({ data: haStatus(scenario === "peerUnreachable") });
      }) as MdkV2Client["systemServiceGetHaStatus"],
      systemServiceGetSystemStatus: (() =>
        refreshFailed()
          ? offline()
          : Promise.resolve({ data: systemStatus() })) as MdkV2Client["systemServiceGetSystemStatus"],
    };
    return new MdkV2Adapter(client as MdkV2Client);
  }, [scenario]);

  return (
    <MinerHostingContext.Provider
      value={{
        adapter,
        resolution: { status: "ready", adapterId: "mdk-v2", adapter },
        minerRoot: "",
        closeButton: null,
        mode: "direct",
        metadata: {},
      }}
    >
      <div className="min-h-screen bg-surface-base p-6 text-text-primary laptop:p-10">
        <div className="mx-auto max-w-[1100px]">
          <ControllerDiagnostics />
        </div>
      </div>
    </MinerHostingContext.Provider>
  );
};

export const HealthyPair = () => <ControllersPreview scenario="healthy" />;
export const PeerUnreachable = () => <ControllersPreview scenario="peerUnreachable" />;
export const HaUnsupported = () => <ControllersPreview scenario="haUnsupported" />;
export const RefreshFails = () => <ControllersPreview scenario="refreshFails" />;

export default { title: "Proto OS/Containers/Controllers", component: ControllerDiagnostics };
