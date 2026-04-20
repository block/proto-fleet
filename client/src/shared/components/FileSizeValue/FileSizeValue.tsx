import { formatFileSize } from "./formatFileSize";

interface FileSizeValueProps {
  value: number;
}

function FileSizeValue({ value }: FileSizeValueProps) {
  return <>{formatFileSize(value)}</>;
}

export default FileSizeValue;
