import * as icons from ".";

export const Icons = () => {
  const iconEntries = Object.entries(icons);

  return (
    <div className="grid auto-rows-fr gap-0" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(250px, 1fr))" }}>
      {iconEntries.map(([name, Icon]) => (
        <div key={name} className="grid grid-cols-[1fr_auto] border border-border-5">
          <div className="flex items-center p-4">
            <span className="overflow-hidden text-ellipsis whitespace-nowrap">{name}</span>
          </div>
          <div className="flex min-w-[64px] items-center justify-center p-4">
            <Icon className="h-5 text-text-primary" width="w-5" />
          </div>
        </div>
      ))}
    </div>
  );
};

export default {
  title: "Foundation/Icons",
};
