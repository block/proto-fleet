import { ControlBoardInfo } from "@/protoOS/api/generatedApi";

export const getControlBoardGeneration = (controlBoardInfo: ControlBoardInfo) => {
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
