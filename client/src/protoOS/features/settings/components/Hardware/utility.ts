import { InternalAsicType } from "./constants";
import { ControlBoardInfo, HashboardInfo } from "@/protoOS/api/types";

export const getControlBoardGeneration = (
  controlBoardInfo: ControlBoardInfo,
) => {
  switch (controlBoardInfo.board_id) {
    case "0": // C1 (proto0)
    case "1": // C1 (evt)
      return 1; // Gen 1
    case "2": // C2 (proto0/1)
    case "3": // C2 (evt)
      return 2; // Gen 2
    case "8": // C3 (proto1)
    case "9": // C3 (PVT)
      return 3; // Gen 3
    default:
      return undefined;
  }
};

export const getHashboardIdentifier = (hashboardInfo: HashboardInfo) => {
  let generation = 1;
  if (
    hashboardInfo.mining_asic === InternalAsicType.MC2 ||
    (hashboardInfo.mining_asic as unknown) === InternalAsicType.MC2Sim
  ) {
    generation = 2;
  }
  return `${hashboardInfo.mining_asic_count}C${generation}`;
};
