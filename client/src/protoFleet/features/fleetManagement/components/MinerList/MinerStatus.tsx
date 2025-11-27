import { ReactNode, useMemo } from "react";
import { create } from "@bufbuild/protobuf";
import {
  ComponentStatus,
  type MinerComponentStatus,
  MinerComponentStatusSchema,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useMiner, useMinerComponentStatus, useMinerDeviceStatus } from "@/protoFleet/store";
import { Alert, ControlBoard, Fan, Hashboard, LightningAlt } from "@/shared/assets/icons";
import StatusCircle, { statuses } from "@/shared/components/StatusCircle";

type MinerStatusProps = {
  deviceIdentifier: string;
  selectedItems?: string[];
};

type ComponentStatusKeys = keyof Omit<MinerComponentStatus, "$typeName" | "$unknown">;

function getComponentStatus(component: ComponentStatusKeys, statusType: "error" | "warning"): ReactNode {
  const componentStatusMap = {
    controlBoard: (
      <>
        <ControlBoard width="w-4" />
        Control Board {statusType === "error" ? "Failure" : "Warning"}
      </>
    ),
    hashBoards: (
      <>
        <Hashboard width="w-4" />
        Hashboard {statusType === "error" ? "Failure" : "Warning"}
      </>
    ),
    fans: (
      <>
        <Fan width="w-4" />
        Fan {statusType === "error" ? "Failure" : "Warning"}
      </>
    ),
    psu: (
      <>
        <LightningAlt width="w-4" />
        Power {statusType === "error" ? "Failure" : "Warning"}
      </>
    ),
  };

  return componentStatusMap[component];
}

const MinerStatus = ({ deviceIdentifier }: MinerStatusProps) => {
  const miner = useMiner(deviceIdentifier);
  const authenticationNeeded = miner?.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
  const componentStatusFromStore = useMinerComponentStatus(deviceIdentifier || "");
  const componentStatus = useMemo(
    () =>
      componentStatusFromStore ||
      create(MinerComponentStatusSchema, {
        hashBoards: ComponentStatus.UNSPECIFIED,
        controlBoard: ComponentStatus.UNSPECIFIED,
        fans: ComponentStatus.UNSPECIFIED,
        psu: ComponentStatus.UNSPECIFIED,
      }),
    [componentStatusFromStore],
  );

  const deviceStatusFromStore = useMinerDeviceStatus(deviceIdentifier || "");

  const status = useMemo(() => {
    if (authenticationNeeded) {
      return (
        <>
          <StatusCircle status={statuses.inactive} variant="simple" width="w-[6px]" />
          Needs Authentication
        </>
      );
    }

    if (deviceStatusFromStore === DeviceStatus.OFFLINE) {
      return (
        <>
          <StatusCircle status={statuses.inactive} variant="simple" width="w-[6px]" />
          Offline
        </>
      );
    }

    if (deviceStatusFromStore === DeviceStatus.INACTIVE) {
      return (
        <>
          <StatusCircle status={statuses.inactive} variant="simple" width="w-[6px]" />
          Sleeping
        </>
      );
    }

    // prioritize showing errors over warnings
    // TODO: determine status with comingled errors and warnings
    const componentErrors = Object.entries(componentStatus).reduce((acc, [key, value]) => {
      if (value === ComponentStatus.ERROR) {
        acc.push(key as ComponentStatusKeys);
      }
      return acc;
    }, [] as ComponentStatusKeys[]);

    // if theres exactly one error, display component name and icon
    if (componentErrors.length === 1) {
      return (
        <>
          <StatusCircle status={statuses.error} variant="simple" width="w-[6px]" />
          {getComponentStatus(componentErrors[0], "error")}
        </>
      );
    }

    // if there are multiple errors, display a generic error message
    if (componentErrors.length > 1) {
      return (
        <>
          <StatusCircle status={statuses.error} variant="simple" width="w-[6px]" />
          <Alert width="w-4" />
          Multiple Failures
        </>
      );
    }

    // if there are no errors, check for warnings
    const componentWarnings = Object.entries(componentStatus).reduce((acc, [key, value]) => {
      if (value === ComponentStatus.WARNING) {
        acc.push(key as ComponentStatusKeys);
      }
      return acc;
    }, [] as ComponentStatusKeys[]);

    // if theres exactly one warning, display component name and icon
    if (componentWarnings.length === 1) {
      return (
        <>
          <StatusCircle status={statuses.warning} variant="simple" width="w-[6px]" />
          {getComponentStatus(componentWarnings[0], "warning")}
        </>
      );
    }

    // if there are multiple warnings, display a generic error message
    if (componentWarnings.length > 1) {
      return (
        <>
          <StatusCircle status={statuses.warning} variant="simple" width="w-[6px]" />
          <Alert width="w-4" />
          Multiple Warnings
        </>
      );
    }

    return (
      <>
        <StatusCircle status={statuses.normal} variant="simple" width="w-[6px]" />
        Hashing
      </>
    );
  }, [authenticationNeeded, deviceStatusFromStore, componentStatus]);

  return <div className="flex items-center gap-1">{status}</div>;
};

export default MinerStatus;
