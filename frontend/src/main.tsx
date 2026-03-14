import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';
import 'antd/dist/reset.css';
import './index.css'; // 引入我们新的全局样式文件
import { initDatabase } from './api/db_sqlite';

// 初始化数据库
initDatabase().catch(err => console.error('Failed to initialize database:', err));

const buildMode = (import.meta as any).env?.MODE;
console.log('[App] mode', buildMode);
console.log('[App] strictMode', true);

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);