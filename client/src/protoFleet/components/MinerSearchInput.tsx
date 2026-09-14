import ListSearchInput, { type ListSearchInputProps } from "@/protoFleet/components/ListSearchInput";

type MinerSearchInputProps = Omit<ListSearchInputProps, "label" | "id"> & { id?: string };

/** The miner-list flavour of ListSearchInput: same debounce, collapse, and
 * sanitization, labelled for miners so every miner picker reads the same. */
const MinerSearchInput = ({ id = "miner-search", ...props }: MinerSearchInputProps) => (
  <ListSearchInput id={id} label="Search miners" {...props} />
);

export default MinerSearchInput;
