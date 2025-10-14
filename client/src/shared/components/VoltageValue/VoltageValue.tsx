interface VoltageValueProps {
  value: number;
}

function VoltageValue({ value }: VoltageValueProps) {
  return <>{value.toFixed(1)} V</>;
}

export default VoltageValue;
