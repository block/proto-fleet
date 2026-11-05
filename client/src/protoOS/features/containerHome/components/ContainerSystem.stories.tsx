import { useEffect, useMemo } from "react";
import { latest } from "../utils/buildContainerHome";
import ContainerOverviewView from "./ContainerOverviewView";
import ContainerSystem from "./ContainerSystem";
import { MdkV2Adapter, type MdkV2Client } from "@/protoOS/api/adapters/mdkV2";
import { toCanonicalContainer } from "@/protoOS/api/adapters/mdkV2/normalize";
import MinerHostingContext from "@/protoOS/contexts/MinerHostingContext/MinerHostingContext";
import useMinerStore from "@/protoOS/store/useMinerStore";

function SystemPreview({ unsupported = false, overview = false }: { unsupported?: boolean; overview?: boolean }) {
  const preview = useMemo(() => {
    const map = { containerMap: { tanks: Array.from({ length: 6 }, (_, index) => ({ id: index + 1 })) } };
    const controls = [
      { name: "coolant_pump", enabled: true, stateKnown: true },
      { name: "dry_cooler", enabled: true, stateKnown: true },
      { name: "dry_cooler_auto", enabled: true, stateKnown: true },
    ];
    const setpoints = [
      { name: "coolant_pump_speed_hz", value: 45, unit: "Hz", min: 30, max: 60, boundsKnown: true },
      { name: "dry_cooler_auto_temp_c", value: 32, unit: "C", min: 20, max: 55, boundsKnown: true },
      { name: "dry_cooler_manual_speed_percent", value: 80, unit: "%", min: 0, max: 100, boundsKnown: true },
    ];
    const health = () => ({
      controls,
      setpoints,
      plant: {
        facilityPowerW: 220000,
        flowRateLpm: 1200,
        outletTemperatureC: 39.25,
        insideAmbientTemperatureC: 28,
        outsideAmbientTemperatureC: 24,
        coolantPumpSpeedHz: controls[0].enabled ? setpoints[0].value : 0,
        inletTemperatureC: 32,
        dryCoolerInletTemperatureC: 39.25,
        outletATemperatureC: 39,
        outletBTemperatureC: 39.5,
        tankALiquidLevelMm: 860,
        tankBLiquidLevelMm: 870,
        pumpOutletPressurePa: 290000,
        heatRejectionOutletPressurePa: 200000,
      },
      coolingFans: Array.from({ length: 10 }, (_, index) => ({
        componentId: `Fan ${index + 1}`,
        speedPercent: !controls[1].enabled ? 0 : controls[2].enabled ? 80 : setpoints[2].value,
        alarmActive: index === 9,
      })),
    });
    const client = {
      hashboardServiceGetContainerMap: async () => ({ data: map }),
      miningServiceGetMiningStatus: async () => ({ data: {} }),
      telemetryServiceGetContainerHealth: async () => ({ data: health() }),
      powerServiceGetContainerControls: async () => ({ data: { supported: !unsupported, controls, setpoints } }),
      powerServiceSetContainerControl: async ({ name }: { name: string }, { enabled }: { enabled: boolean }) => {
        const control = controls.find((item) => item.name === name)!;
        control.enabled = enabled;
        return { data: { control } };
      },
      powerServiceSetContainerSetpoint: async ({ name }: { name: string }, { value }: { value: number }) => {
        const setpoint = setpoints.find((item) => item.name === name)!;
        setpoint.value = value;
        return { data: { setpoint } };
      },
    };
    return {
      adapter: new MdkV2Adapter(client as unknown as MdkV2Client, { deviceId: "ct1-story" }),
      fixture: toCanonicalContainer(map, {}, health(), "ct1-story"),
    };
  }, [unsupported]);
  useEffect(() => {
    const store = useMinerStore.getState();
    store.resetDeviceData();
    store.hardware.replaceHardware(preview.fixture.hardware);
    store.telemetry.mergeTelemetry(preview.fixture.telemetry);
    return () => useMinerStore.getState().resetDeviceData();
  }, [preview]);
  const hardware = useMinerStore((state) => state.hardware);
  const telemetry = useMinerStore((state) => state.telemetry);
  const plantId = [...hardware.plant.values()].find((item) => item.kind === "plant")?.ref.id;
  const plant = plantId ? telemetry.plant.get(plantId) : undefined;
  const fans = [...hardware.fans.values()];
  return (
    <MinerHostingContext.Provider
      value={{
        adapter: preview.adapter,
        resolution: { status: "ready", adapterId: "mdk-v2", adapter: preview.adapter },
        minerRoot: "",
        closeButton: null,
        mode: "direct",
        metadata: {},
      }}
    >
      <div className="min-h-screen bg-surface-base p-6 text-text-primary">
        {overview ? (
          <ContainerOverviewView plant={plant} fans={fans} power={latest(plant?.facilityPower)} />
        ) : (
          <ContainerSystem plant={plant} fans={fans} />
        )}
      </div>
    </MinerHostingContext.Provider>
  );
}

export const Overview = () => <SystemPreview overview />;
export const Interactive = () => <SystemPreview />;
export const UnsupportedControls = () => <SystemPreview unsupported />;
export default { title: "Proto OS/Containers/Home/Live system", component: ContainerSystem };
