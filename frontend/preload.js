const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('electronAPI', {
  minimize: () => ipcRenderer.send('window-minimize'),
  maximize: () => ipcRenderer.send('window-maximize'),
  close: () => ipcRenderer.send('window-close'),
  db: {
    execute: (sql, params) => ipcRenderer.invoke('db-execute', sql, params),
    query: (sql, params) => ipcRenderer.invoke('db-query', sql, params),
    queryOne: (sql, params) => ipcRenderer.invoke('db-query-one', sql, params)
  },
  fs: {
    ensureDir: (dirPath) => ipcRenderer.invoke('fs-ensure-dir', dirPath),
    saveFile: (filePath, buffer) => ipcRenderer.invoke('fs-save-file', filePath, buffer),
    readFile: (filePath) => ipcRenderer.invoke('fs-read-file', filePath),
    exists: (filePath) => ipcRenderer.invoke('fs-exists', filePath),
    getPath: (name) => ipcRenderer.invoke('fs-get-path', name),
    pathJoin: (...args) => ipcRenderer.invoke('fs-path-join', ...args)
  }
});

contextBridge.exposeInMainWorld("versions", {
    node: () => process.versions.node,
    chrome: () => process.versions.chrome,
    electron: () => process.versions.electron,
});
