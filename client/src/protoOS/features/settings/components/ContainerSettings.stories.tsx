import { type ReactNode, useMemo } from "react";
import ContainerCooling from "./General/ContainerCooling";
import ContainerMiningPools from "./MiningPools/ContainerMiningPools";
import { MdkV2Adapter, type MdkV2Client } from "@/protoOS/api/adapters/mdkV2";
import MinerHostingContext from "@/protoOS/contexts/MinerHostingContext/MinerHostingContext";

const ContainerPreview = ({ children }: { children: ReactNode }) => {
  const adapter = useMemo(() => {
    let nextPoolId = 4;
    let pools = [1, 2, 3].map((id) => ({
      id: String(id),
      name: `Client pool A${id}`,
      url: `stratum+tcp://pool${id}.example.com:3333`,
      username: "mann23.workerbee",
      priority: id - 1,
      enabled: true,
    }));
    const controls = [
      { name: "coolant_pump", stateKnown: true, enabled: true },
      { name: "dry_cooler", stateKnown: true, enabled: true },
      { name: "dry_cooler_auto", stateKnown: true, enabled: true },
    ];
    const setpoints = [
      { name: "coolant_pump_speed_hz", value: 45, unit: "Hz", boundsKnown: true, min: 30, max: 60 },
      { name: "dry_cooler_auto_temp_c", value: 38, unit: "C", boundsKnown: true, min: 20, max: 55 },
    ];
    const client: { [K in keyof MdkV2Client]?: (...args: Parameters<MdkV2Client[K]>) => Promise<unknown> } = {
      poolServiceListPools: async () => ({ data: { pools } }),
      poolServiceUpdatePool: async ({ poolId }, { pool }) => {
        pools = pools.map((current) => (current.id === poolId ? { ...current, ...pool } : current));
      },
      poolServiceDeletePool: async ({ id }) => {
        pools = pools.filter((pool) => pool.id !== id);
      },
      poolServiceCreatePool: async ({ pool }) => {
        pools.push({ ...pool, id: String(nextPoolId++) } as (typeof pools)[number]);
      },
      powerServiceGetContainerControls: async () => ({ data: { supported: true, controls, setpoints } }),
      powerServiceSetContainerControl: async ({ name }, { enabled }) => {
        controls.find((control) => control.name === name)!.enabled = enabled!;
      },
      powerServiceSetContainerSetpoint: async ({ name }, { value }) => {
        setpoints.find((setting) => setting.name === name)!.value = value!;
      },
    };
    return new MdkV2Adapter(client as MdkV2Client);
  }, []);
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
      <div className="mx-auto max-w-160 p-6 tablet:py-20">{children}</div>
    </MinerHostingContext.Provider>
  );
};

export const Pools = () => (
  <ContainerPreview>
    <ContainerMiningPools />
  </ContainerPreview>
);
export const Cooling = () => (
  <ContainerPreview>
    <ContainerCooling />
  </ContainerPreview>
);
export default { title: "Proto OS/Settings/Container controls" };
