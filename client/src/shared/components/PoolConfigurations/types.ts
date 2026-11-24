export const poolSlotLabel = (index: number) => (index === 0 ? "Primary pool" : `Backup ${index}`);
export interface ConfigurationPoolDraft {
  key: string;
  url: string;
  username: string;
  password: string;
  preservePassword: boolean;
}
