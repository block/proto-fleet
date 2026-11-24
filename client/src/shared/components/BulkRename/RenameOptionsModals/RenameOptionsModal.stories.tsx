import { action } from "storybook/actions";
import RenameOptionsModal, { RenameOptionsModalBody } from "./RenameOptionsModal";
import Input from "@/shared/components/Input";

export default {
  title: "Proto Fleet/Fleet Management/RenameOptionsModal",
  component: RenameOptionsModal,
};

export const Default = () => (
  <RenameOptionsModal
    onConfirm={() => action("onConfirm")()}
    onDismiss={() => action("onDismiss")()}
    desktopSaveTestId="save-desktop"
    mobileSaveTestId="save-mobile"
  >
    <RenameOptionsModalBody>
      <Input id="prefix" label="Prefix" initValue="Miner" onChange={() => {}} />
      <Input id="separator" label="Separator" initValue="-" onChange={() => {}} />
    </RenameOptionsModalBody>
  </RenameOptionsModal>
);

export const SaveDisabled = () => (
  <RenameOptionsModal
    onConfirm={() => action("onConfirm")()}
    onDismiss={() => action("onDismiss")()}
    desktopSaveTestId="save-desktop"
    mobileSaveTestId="save-mobile"
    saveDisabled
  >
    <RenameOptionsModalBody>
      <Input id="prefix" label="Prefix" initValue="" onChange={() => {}} />
    </RenameOptionsModalBody>
  </RenameOptionsModal>
);
