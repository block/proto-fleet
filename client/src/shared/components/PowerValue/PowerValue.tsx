interface PowerValueProps {
  value: number;
}

function PowerValue({ value }: PowerValueProps) {
  return <>{value.toFixed(1)} W</>;
}

export default PowerValue;
