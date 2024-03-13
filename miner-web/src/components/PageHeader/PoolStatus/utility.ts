interface getTextsProps {
  isConnected: boolean;
  priority?: number;
  url?: string;
}

export const getTexts = ({ isConnected, priority, url }: getTextsProps) => {
  if (!url) {
    return {
      title: "No mining pools",
      subtitle: "Add a mining pool to collect mining rewards.",
      button: "Add mining pools",
      cardTitle: undefined,
    };
  }
  if (isConnected) {
    return {
      title: "Mining pool",
      subtitle: `This miner is active and connected to your ${priority === 0 ? "default" : "backup"} mining pool.`,
      button: "View mining pools",
      cardTitle: "Connected",
    };
  }

  return {
    title: "Mining pool",
    subtitle: "This miner has lost connection to all mining pools.",
    button: "View mining pools",
    cardTitle: "Not connected",
  };
};
