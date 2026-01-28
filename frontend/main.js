const { app, BrowserWindow, ipcMain } = require('electron');
const path = require('path');

// 判断是否为开发环境
const isDev = process.env.NODE_ENV !== 'production';

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
}

app.whenReady().then(() => {
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
