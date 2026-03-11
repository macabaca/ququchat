export interface ISqliteClient {
    execute(sql: string, params?: any[]): Promise<any>;
    query<T>(sql: string, params?: any[]): Promise<T[]>;
    queryOne<T>(sql: string, params?: any[]): Promise<T | null>;
}

export class IpcSqliteClient implements ISqliteClient {
    private getDb() {
        const db = window.electronAPI?.db;
        if (!db) {
            throw new Error('SQLite IPC 未初始化：请通过 Electron 启动应用（npm run electron），不要直接用浏览器访问');
        }
        return db;
    }

    async execute(sql: string, params: any[] = []): Promise<any> {
        try {
            return await this.getDb().execute(sql, params);
        } catch (error) {
            console.error('SQLite execute error:', error);
            throw error;
        }
    }

    async query<T>(sql: string, params: any[] = []): Promise<T[]> {
        try {
            return await this.getDb().query(sql, params);
        } catch (error) {
            console.error('SQLite query error:', error);
            throw error;
        }
    }

    async queryOne<T>(sql: string, params: any[] = []): Promise<T | null> {
        try {
            return await this.getDb().queryOne(sql, params);
        } catch (error) {
            console.error('SQLite queryOne error:', error);
            throw error;
        }
    }
}

// 导出一个单例实例
export const sqliteClient = new IpcSqliteClient();
