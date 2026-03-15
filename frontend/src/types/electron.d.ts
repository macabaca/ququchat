export interface IElectronAPI {
  minimize: () => void;
  maximize: () => void;
  close: () => void;
  db: {
    execute: (sql: string, params?: any[]) => Promise<any>;
    query: (sql: string, params?: any[]) => Promise<any[]>;
    queryOne: (sql: string, params?: any[]) => Promise<any>;
  };
  fs: {
    ensureDir: (dirPath: string) => Promise<boolean>;
    saveFile: (filePath: string, buffer: Uint8Array) => Promise<boolean>;
    readFile: (filePath: string) => Promise<Uint8Array>;
    exists: (filePath: string) => Promise<boolean>;
    getPath: (name: string) => Promise<string>;
    pathJoin: (...args: string[]) => Promise<string>;
    showSaveDialog: (options?: {
      title?: string;
      defaultPath?: string;
      filters?: Array<{ name: string; extensions: string[] }>;
    }) => Promise<{ canceled: boolean; filePath: string }>;
  };
}

declare global {
  interface Window {
    electronAPI: IElectronAPI;
  }
}
