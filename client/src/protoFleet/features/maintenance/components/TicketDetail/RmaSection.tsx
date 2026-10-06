import Input from "@/shared/components/Input";

const RmaSection = () => {
  // TODO: wire to ticket data — only shown when status is SENT_TO_VENDOR
  return null;
};

export const RmaSectionContent = ({
  vendor,
  tracking,
  eta,
  onVendorChange,
  onTrackingChange,
  onEtaChange,
}: {
  vendor: string;
  tracking: string;
  eta: string;
  onVendorChange: (value: string) => void;
  onTrackingChange: (value: string) => void;
  onEtaChange: (value: string) => void;
}) => (
  <div className="flex flex-col gap-3">
    <span className="text-emphasis-300 font-medium">RMA Details</span>
    <div className="grid grid-cols-3 gap-3">
      <Input id="rma-vendor" label="Vendor" initValue={vendor} onChange={onVendorChange} />
      <Input id="rma-tracking" label="Tracking #" initValue={tracking} onChange={onTrackingChange} />
      <Input id="rma-eta" label="ETA" initValue={eta} onChange={onEtaChange} type="date" />
    </div>
  </div>
);

export default RmaSection;
