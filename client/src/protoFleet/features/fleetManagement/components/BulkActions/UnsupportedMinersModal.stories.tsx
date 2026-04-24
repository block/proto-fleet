import { useState } from "react";
import { create } from "@bufbuild/protobuf";
import { action } from "storybook/actions";
import UnsupportedMinersModal from "./UnsupportedMinersModal";
import { UnsupportedMinerGroupSchema } from "@/protoFleet/api/generated/minercommand/v1/command_pb";

export default {
  title: "Proto Fleet/Fleet Management/UnsupportedMinersModal",
  component: UnsupportedMinersModal,
};

const mockGroups = [
  create(UnsupportedMinerGroupSchema, {
    firmwareVersion: "1.2.3",
    model: "S19 Pro",
    count: 5,
  }),
  create(UnsupportedMinerGroupSchema, {
    firmwareVersion: "1.1.0",
    model: "S19j Pro",
    count: 3,
  }),
];

export const WithSomeSupported = () => {
  const [open, setOpen] = useState(true);

  return (
    <>
      {!open ? (
        <div className="flex h-screen items-center justify-center">
          <button onClick={() => setOpen(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
            Show Modal
          </button>
        </div>
      ) : null}
      <UnsupportedMinersModal
        open={open}
        unsupportedGroups={mockGroups}
        totalUnsupportedCount={8}
        noneSupported={false}
        onContinue={() => {
          action("onContinue")();
          setOpen(false);
        }}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
      />
    </>
  );
};

export const NoneSupported = () => {
  const [open, setOpen] = useState(true);

  return (
    <>
      {!open ? (
        <div className="flex h-screen items-center justify-center">
          <button onClick={() => setOpen(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
            Show Modal
          </button>
        </div>
      ) : null}
      <UnsupportedMinersModal
        open={open}
        unsupportedGroups={mockGroups}
        totalUnsupportedCount={8}
        noneSupported={true}
        onContinue={() => action("onContinue")()}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
      />
    </>
  );
};

export const SingleMiner = () => {
  const [open, setOpen] = useState(true);

  const singleGroup = [
    create(UnsupportedMinerGroupSchema, {
      firmwareVersion: "1.0.0",
      model: "S19 XP",
      count: 1,
    }),
  ];

  return (
    <>
      {!open ? (
        <div className="flex h-screen items-center justify-center">
          <button onClick={() => setOpen(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
            Show Modal
          </button>
        </div>
      ) : null}
      <UnsupportedMinersModal
        open={open}
        unsupportedGroups={singleGroup}
        totalUnsupportedCount={1}
        noneSupported={false}
        onContinue={() => {
          action("onContinue")();
          setOpen(false);
        }}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
      />
    </>
  );
};
