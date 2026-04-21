interface SlotNumberProps {
  number: number;
}

const SlotNumber = ({ number }: SlotNumberProps) => {
  return (
    <div className="grid aspect-square w-5 place-items-center rounded-full bg-core-primary-10 text-xs font-medium text-text-primary">
      {number}
    </div>
  );
};

export default SlotNumber;
