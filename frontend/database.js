const initSqlJs = require('sql.js');
const fs = require('fs');
const path = require('path');
const { app } = require('electron');

let db = null;
let dbPath = null;
let SQL = null;

async function initDatabase() {
  if (db) return db;

  // 1. 确定数据库路径
  // 兼容开发环境和打包环境
  // 注意：sql.js 的 wasm 文件通常需要从 node_modules 复制，但在 Node 环境下它会自动加载
  
  dbPath = path.join(app.getPath('userData'), 'ququchat.sqlite');
  console.log(`[Database] Initializing sql.js at: ${dbPath}`);

  try {
    // 2. 加载 SQL.js 引擎
    SQL = await initSqlJs();

    // 3. 读取现有数据库文件（如果存在）
    if (fs.existsSync(dbPath)) {
      const filebuffer = fs.readFileSync(dbPath);
      db = new SQL.Database(filebuffer);
      console.log('[Database] Loaded existing database from disk');
    } else {
      db = new SQL.Database();
      console.log('[Database] Created new in-memory database');
      saveDatabase(); // 初始化空文件
    }
    
    return db;
  } catch (err) {
    console.error('[Database] Failed to initialize:', err);
    throw err;
  }
}

// 将内存数据库保存到磁盘
function saveDatabase() {
  if (!db || !dbPath) return;
  try {
    const data = db.export();
    const buffer = Buffer.from(data);
    fs.writeFileSync(dbPath, buffer);
  } catch (err) {
    console.error('[Database] Failed to save to disk:', err);
  }
}

// 辅助函数：将 sql.js 的数组格式转换为对象数组 [{col: val}, ...]
function transformResult(result) {
  if (!result || result.length === 0) return [];
  const { columns, values } = result[0];
  return values.map(row => {
    const obj = {};
    columns.forEach((col, i) => {
      obj[col] = row[i];
    });
    return obj;
  });
}

function execute(sql, params = []) {
  if (!db) throw new Error('Database not initialized');
  try {
    // sql.js 的 run 返回的是 database 对象本身，而不是 changes
    // 为了获取 lastInsertRowid，我们需要执行额外的查询，或者使用 exec
    // 这里简化处理，先执行 run
    db.run(sql, params);
    
    // 尝试获取受影响行数或插入ID（sql.js 不直接返回这些，模拟一下）
    // 如果是 INSERT，通常需要 select last_insert_rowid()
    let lastInsertRowid = 0;
    if (sql.trim().toUpperCase().startsWith('INSERT')) {
       const res = db.exec('SELECT last_insert_rowid()');
       if (res[0] && res[0].values[0]) {
         lastInsertRowid = res[0].values[0][0];
       }
    }

    // 立即保存到磁盘 (简单粗暴的持久化策略)
    saveDatabase();

    return { changes: 1, lastInsertRowid }; 
  } catch (err) {
    console.error('[Database] Execute error:', err);
    throw err;
  }
}

function query(sql, params = []) {
  if (!db) throw new Error('Database not initialized');
  try {
    // 使用 bind + step 模式或者直接 exec
    // sql.js 的 exec 不支持绑定参数，需要使用 prepare
    const stmt = db.prepare(sql);
    stmt.bind(params);
    
    const rows = [];
    while (stmt.step()) {
      rows.push(stmt.getAsObject());
    }
    stmt.free(); // 释放内存
    
    return rows;
  } catch (err) {
    console.error('[Database] Query error:', err);
    throw err;
  }
}

function queryOne(sql, params = []) {
  const rows = query(sql, params);
  return rows.length > 0 ? rows[0] : null;
}

module.exports = {
  initDatabase,
  execute,
  query,
  queryOne
};
