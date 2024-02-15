import InfoWidgetWrapperComponent from "./InfoWidgetWrapper";
import InfoWidgetComponent from ".";

export const InfoWidget = () => {
  return <InfoWidgetComponent title="Current Power Usage" value="3.6 kW" />;
};

export const InfoWidgetWrapper = () => {
  return (
    <div className="w-1/2">
      <InfoWidgetWrapperComponent>
        <InfoWidgetComponent title="Current Power Usage" value="3.6 kW" />
        <InfoWidgetComponent
          title="Average Fan Speed"
          value="3,980 RPM"
          className="text-text-critical"
        />
        <InfoWidgetComponent
          title="Average ASIC Temperature"
          value="30.56&deg;c"
        />
      </InfoWidgetWrapperComponent>
    </div>
  );
};

export default {
  component: InfoWidget,
  title: "Info Widget",
};
