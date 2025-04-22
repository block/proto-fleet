import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";
import { ErrorLevel } from "./constants";
import MinerStatusModalComponent from "./MinerStatusModal";
import { type NotificationError } from "@/protoOS/api/types";

const mockErrs = {
  hashboardErr: {
    error_code: "HashboardError",
    error_level: ErrorLevel.error,
    error_message: "Hashboard error message",
  },
  hashboardWarning: {
    error_code: "HashboardWarning",
    error_level: ErrorLevel.warning,
    error_message: "Hashboard warning message",
  },
  psuErr: {
    error_code: "PSUError",
    error_level: ErrorLevel.error,
    error_message: "PSU error message",
  },
  psuWarning: {
    error_code: "PSUWarning",
    error_level: ErrorLevel.warning,
    error_message: "PSU warning message",
  },
  fanErr: {
    error_code: "FanError",
    error_level: ErrorLevel.error,
    error_message: "Fan error message",
  },
  fanWarning: {
    error_code: "FanWarning",
    error_level: ErrorLevel.warning,
    error_message: "Fan warning message",
  },
  controlBoardErr: {
    error_code: "ControlBoardError",
    error_level: ErrorLevel.error,
    error_message: "Control Board error message",
  },
  controlBoardWarning: {
    error_code: "ControlBoardWarning",
    error_level: ErrorLevel.warning,
    error_message: "Control Board warning message",
  },
};

// Create error arrays for different scenarios
const noErrors: NotificationError[] = [];
const oneError: NotificationError[] = [mockErrs.fanErr];
const multipleErrors: NotificationError[] = [
  mockErrs.fanErr,
  mockErrs.hashboardErr,
  mockErrs.psuErr,
];
const oneWarning: NotificationError[] = [mockErrs.fanWarning];
const multipleWarnings: NotificationError[] = [
  mockErrs.fanWarning,
  mockErrs.hashboardWarning,
  mockErrs.psuWarning,
];
const mixedErrorsWarnings: NotificationError[] = [
  mockErrs.fanErr,
  mockErrs.hashboardWarning,
  mockErrs.psuErr,
  mockErrs.controlBoardWarning,
];

const errorStates = {
  noErrors,
  oneError,
  multipleErrors,
  oneWarning,
  multipleWarnings,
  mixedErrorsWarnings,
} as const;

type ErrorState = keyof typeof errorStates;
export const MinerStatusModal = ({
  errorState,
}: {
  errorState: ErrorState;
}) => {
  return (
    <div>
      <MinerStatusModalComponent
        onDismiss={() => null}
        errors={errorStates[errorState]}
      />
    </div>
  );
};

MinerStatusModal.args = {
  errorState: "noErrors",
};

MinerStatusModal.argTypes = {
  errorState: {
    control: "select",
    options: Object.keys(errorStates),
    description: "Select error state to display",
  },
};

export default {
  title: "ProtoOS/MinerStatusModal",
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
