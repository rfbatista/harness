/** Where the page keeps its scope: the address's ?repository=. "" is the project defaults. */
export interface ScopeAddress {
  read(): string;
  write(repositoryId: string): void;
  /** Calls `listener` when Back or Forward changes the scope; returns what stops it. */
  follow(listener: (repositoryId: string) => void): () => void;
}
