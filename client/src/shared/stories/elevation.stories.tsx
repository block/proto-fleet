const ElevationBlock = ({ elevation }: { elevation: number }) => {
  return (
    <>
      <div>Elevation {elevation}</div>
      <div className={`p-12 shadow-${elevation} text-center`}>
        <div className="text-300">shadow-{elevation}</div>
      </div>
    </>
  );
};

export const Elevation = () => {
  return (
    <div className="flex w-96 flex-col space-y-6">
      <ElevationBlock elevation={50} />
      <ElevationBlock elevation={100} />
      <ElevationBlock elevation={200} />
      <ElevationBlock elevation={300} />
    </div>
  );
};

export default {
  title: "Foundation/Elevation",
};
