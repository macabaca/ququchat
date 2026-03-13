const { app, BrowserWindow, ipcMain } = require('electron');
const path = require('path');
const fs = require('fs').promises;
const db = require('./database'); // 引入数据库模块

// 判断是否为开发环境
const isDev = !app.isPackaged;

function createWindow() {
  const preloadPath = path.join(app.getAppPath(), 'electron', 'preload.js');
  // Fallback if the path above is incorrect (since preload.js is in root in previous ls)
  // Checking previous ls: d:\ququchat\frontend\preload.js
  // So it should be path.join(__dirname, 'preload.js')
  const actualPreloadPath = path.join(__dirname, 'preload.js');
  console.log(`[Electron Main] Preload script path: ${actualPreloadPath}`);

  const win = new BrowserWindow({
    width: 1080,           // 增加宽度，提供更宽敞的布局
    height: 720,          // 增加高度
    minWidth: 800,        // 最小宽度
    minHeight: 600,       // 最小高度
    frame: false,         // 无边框窗口，实现自定义标题栏
    resizable: true,      // 允许调整大小
    center: true,
    backgroundColor: '#00000000', // 透明背景，允许圆角
    webPreferences: {
      preload: actualPreloadPath,
      contextIsolation: true,
      nodeIntegration: false,
    },
  });

  // 移除默认菜单
  win.setMenu(null);

  if (isDev) {
    win.loadURL('http://localhost:5173');
    // win.webContents.openDevTools(); 
  } else {
    win.loadFile(path.join(__dirname, 'dist/index.html'));
  }

  // IPC 监听：窗口控制
  ipcMain.on('window-minimize', () => {
    win.minimize();
  });

  ipcMain.on('window-maximize', () => {
    if (win.isMaximized()) {
      win.unmaximize();
    } else {
      win.maximize();
    }
  });

  ipcMain.on('window-close', () => {
    win.close();
  });

  // IPC 监听：数据库操作
  ipcMain.handle('db-execute', async (event, sql, params) => {
    try {
      return db.execute(sql, params);
    } catch (err) {
      console.error('IPC db-execute error:', err);
      throw err;
    }
  });

  ipcMain.handle('db-query', async (event, sql, params) => {
    try {
      return db.query(sql, params);
    } catch (err) {
      console.error('IPC db-query error:', err);
      throw err;
    }
  });

  ipcMain.handle('db-query-one', async (event, sql, params) => {
    try {
      return db.queryOne(sql, params);
    } catch (err) {
      console.error('IPC db-query-one error:', err);
      throw err;
    }
  });

  // IPC 监听：文件系统操作
  ipcMain.handle('fs-ensure-dir', async (event, dirPath) => {
    try {
      await fs.mkdir(dirPath, { recursive: true });
      return true;
    } catch (err) {
      console.error('IPC fs-ensure-dir error:', err);
      throw err;
    }
  });

  ipcMain.handle('fs-save-file', async (event, filePath, buffer) => {
    try {
      let nodeBuffer;
      if (Buffer.isBuffer(buffer)) {
        nodeBuffer = buffer;
      } else if (buffer instanceof Uint8Array) {
        nodeBuffer = Buffer.from(buffer);
      } else if (buffer instanceof ArrayBuffer) {
        nodeBuffer = Buffer.from(new Uint8Array(buffer));
      } else if (buffer?.type === 'Buffer' && Array.isArray(buffer?.data)) {
        nodeBuffer = Buffer.from(buffer.data);
      } else {
        throw new Error(`Unsupported buffer type for fs-save-file: ${typeof buffer}`);
      }

      await fs.mkdir(path.dirname(filePath), { recursive: true });
      await fs.writeFile(filePath, nodeBuffer);

      const stat = await fs.stat(filePath);
      if (!stat || stat.size <= 0) {
        throw new Error(`File persisted with invalid size: ${filePath}`);
      }
      return true;
    } catch (err) {
      console.error('IPC fs-save-file error:', err);
      throw err;
    }
  });

  ipcMain.handle('fs-read-file', async (event, filePath) => {
    try {
      const data = await fs.readFile(filePath);
      return data;
    } catch (err) {
      console.error('IPC fs-read-file error:', err);
      throw err;
    }
  });

  ipcMain.handle('fs-exists', async (event, filePath) => {
    try {
      await fs.access(filePath);
      return true;
    } catch (err) {
      return false;
    }
  });

  ipcMain.handle('fs-get-path', async (event, name) => {
    try {
      return app.getPath(name);
    } catch (err) {
      console.error('IPC fs-get-path error:', err);
      throw err;
    }
  });

  ipcMain.handle('fs-path-join', async (event, ...args) => {
    try {
      return path.join(...args);
    } catch (err) {
      console.error('IPC fs-path-join error:', err);
      throw err;
    }
  });
}

app.whenReady().then(async () => {
  try {
    await db.initDatabase();
    console.log('Database initialized');
  } catch (err) {
    console.error('Failed to initialize database:', err);
  }

  createWindow();

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createWindow();
    }
  });
});

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') {
    app.quit();
  }
});
