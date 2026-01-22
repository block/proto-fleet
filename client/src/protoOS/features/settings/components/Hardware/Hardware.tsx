import { getControlBoardGeneration } from "./utility";
import { useCoolingStatus, useHardware } from "@/protoOS/api";
import { TOTAL_FAN_SLOTS, TOTAL_HASHBOARD_SLOTS, TOTAL_PSU_SLOTS } from "@/protoOS/api/constants";
import { useCoolingMode } from "@/protoOS/store";
import { areAllFansDisconnected } from "@/protoOS/store/utils/coolingUtils";
import { FanIndicator, HashboardIndicator, Info, PsuIndicator } from "@/shared/assets/icons";
import Callout, { intents } from "@/shared/components/Callout";
import { DataNullState } from "@/shared/components/DataNullState";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";
import SkeletonBar from "@/shared/components/SkeletonBar";
import SlotNumber from "@/shared/components/SlotNumber";

const Hardware = () => {
  // TODO: [STORE_REFACTOR] Remove this useHardware call once we update this page to read directly from the store
  // Hardware data is now populated by useHardware in AppWrapper.tsx
  const { hashboardsInfo, controlBoardInfo, fansInfo, psusInfo, pending, error } = useHardware();
  const coolingMode = useCoolingMode();

  // Use cooling API for reliable fan detection (hardware API has null placeholders from unimplemented fan calibration)
  const { data: coolingData } = useCoolingStatus();

  const skeletonBar = <SkeletonBar className="h-[22px] w-20" />;

  const noFansConnected = areAllFansDisconnected(coolingData?.fans);
  const isImmersionMode = coolingMode === "Off";
  const showNoFansCallout = noFansConnected && isImmersionMode;

  if (pending) {
    return (
      <>
        <h2 className="mb-10 text-heading-300">Hardware</h2>
        <div className="flex justify-center">
          <ProgressCircular className="my-5" indeterminate />
        </div>
      </>
    );
  }

  if (error) {
    return (
      <>
        <h2 className="mb-10 text-heading-300">Hardware</h2>
        <DataNullState title="Could not load hardware details" description="Test your connection and try again." />
      </>
    );
  }

  return (
    <>
      <h2 className="mb-10 text-heading-300">Hardware</h2>
      <div className="mb-10">
        <h3 className="mb-2 text-heading-100">Control Board</h3>
        <Row className="flex" attributes={{ role: "row" }}>
          <h4 className="w-92 text-emphasis-300">Type</h4>
          <h4 className="w-46 text-emphasis-300">Serial number</h4>
        </Row>
        <Row className="flex items-center">
          <div className="w-92 text-300">
            {controlBoardInfo?.board_id
              ? `Control Board ${getControlBoardGeneration(controlBoardInfo) ?? "Unknown"}`
              : skeletonBar}
          </div>
          <div className="w-46 text-300">{controlBoardInfo?.serial_number ?? skeletonBar}</div>
        </Row>
      </div>
      <div className="mb-10" role="table">
        <h3 className="mb-2 text-heading-100">Hashboards</h3>
        <Row className="flex" attributes={{ role: "row" }}>
          <h4 className="w-46 text-emphasis-300">Position</h4>
          <h4 className="w-46 text-emphasis-300">Hashboard</h4>
          <h4 className="w-46 text-emphasis-300">Serial Number</h4>
        </Row>
        {hashboardsInfo?.map((hashboard, idx) => {
          const slotNumber = idx + 1;
          return (
            <Row key={idx} className="flex items-center" attributes={{ role: "row" }}>
              <div className="flex w-46 items-center gap-2 text-300">
                <SlotNumber number={slotNumber} />
                <HashboardIndicator activeHashboardSlot={slotNumber} totalHashboards={TOTAL_HASHBOARD_SLOTS} />
              </div>
              {hashboard ? (
                <>
                  <div className="w-46 text-300">Model {hashboard.board}</div>
                  <div className="w-46 text-300">{hashboard.hb_sn}</div>
                </>
              ) : (
                <>
                  <div className="w-46 text-300 text-text-primary-70">No component found</div>
                  <div className="w-46 text-300 text-text-primary-70">—</div>
                </>
              )}
            </Row>
          );
        })}
      </div>
      <div className="mb-10">
        <h3 className="mb-2 text-heading-100">Fans</h3>
        {showNoFansCallout ? (
          <Callout
            intent={intents.default}
            prefixIcon={<Info />}
            title="No fans connected"
            subtitle="This miner is set to immersion cooling"
          />
        ) : (
          <>
            <Row className="flex" attributes={{ role: "row" }}>
              <h4 className="w-46 text-emphasis-300">Position</h4>
              <h4 className="w-46 text-emphasis-300">Fan</h4>
            </Row>
            {fansInfo?.map((fan, idx) => {
              const fanPosition = idx + 1;
              return (
                <Row className="flex items-center" key={idx} attributes={{ role: "row" }}>
                  <div className="flex w-46 items-center gap-2 text-300">
                    <SlotNumber number={fanPosition} />
                    <FanIndicator fanPosition={fanPosition} numFans={TOTAL_FAN_SLOTS} />
                  </div>
                  {fan ? (
                    <div className="w-46 text-300">Fan {fan.slot}</div>
                  ) : (
                    <div className="w-46 text-300 text-text-primary-70">No component found</div>
                  )}
                </Row>
              );
            })}
          </>
        )}
      </div>
      <div className="mb-10">
        <h3 className="mb-2 text-heading-100">Power supply</h3>
        <Row className="flex" attributes={{ role: "row" }}>
          <h4 className="w-46 text-emphasis-300">Position</h4>
          <h4 className="w-46 text-emphasis-300">PSU</h4>
          <h4 className="w-46 text-emphasis-300">Serial number</h4>
        </Row>
        {psusInfo?.map((psu, idx) => {
          const slotNumber = idx + 1;
          return (
            <Row className="flex items-center" key={idx} attributes={{ role: "row" }}>
              <div className="flex w-46 items-center gap-2 text-300">
                <SlotNumber number={slotNumber} />
                <PsuIndicator totalSlots={TOTAL_PSU_SLOTS} slotPlacement={slotNumber} />
              </div>
              {psu ? (
                <>
                  <div className="w-46 text-300">Model {psu.model}</div>
                  <div className="w-46 text-300">{psu.psu_sn}</div>
                </>
              ) : (
                <>
                  <div className="w-46 text-300 text-text-primary-70">No component found</div>
                  <div className="w-46 text-300 text-text-primary-70">—</div>
                </>
              )}
            </Row>
          );
        })}
      </div>
    </>
  );
};

export default Hardware;
