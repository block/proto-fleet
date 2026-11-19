interface getTextsProps {
  index?: number;
  isConnected: boolean;
  showPoolStatusLink: boolean;
  url?: string;
}

export const getTexts = ({ index, isConnected, showPoolStatusLink, url }: getTextsProps) => {
  if (showPoolStatusLink) {
    return { button: "View pools" };
  }
  if (!url) {
    return {
      title: "No mining pools",
      subtitle: "Add a mining pool to collect mining rewards.",
      button: "Add mining pools",
    };
  }
  return {
    title: "Mining pool",
    subtitle: isConnected
      ? `This miner is active and connected to your ${index === 0 ? "default" : "backup"} mining pool.`
      : "This miner has lost connection to all mining pools.",
    button: "View mining pools",
  };
};
