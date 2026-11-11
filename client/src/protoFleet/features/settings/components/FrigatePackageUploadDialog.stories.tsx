import { useState } from "react";
import { action } from "storybook/actions";
import FrigatePackageUploadDialog from "./FrigatePackageUploadDialog";

export default {
  title: "Proto Fleet/Settings/FrigatePackageUploadDialog",
  component: FrigatePackageUploadDialog,
};

export const Default = () => {
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
      <FrigatePackageUploadDialog
        open={open}
        onSuccess={(result) => {
          action("onSuccess")(result);
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
