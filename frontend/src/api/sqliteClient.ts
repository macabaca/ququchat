export interface ISqliteClient {
    execute(sql: string, params?: any[]): Promise<any>;
    query<T>(sql: string, params?: any[]): Promise<T[]>;
    queryOne<T>(sql: string, params?: any[]): Promise<T | null>;
}

export class IpcSqliteClient implements ISqliteClient {
    async execute(sql: string, params: any[] = []): Promise<any> {
        try {
            return await window.electronAPI.db.execute(sql, params);
        } catch (error) {
            console.error('SQLite execute error:', error);
            throw error;
        }
    }

    async query<T>(sql: string, params: any[] = []): Promise<T[]> {
        try {
            return await window.electronAPI.db.query(sql, params);
        } catch (error) {
            console.error('SQLite query error:', error);
            throw error;
        }
    }

    async queryOne<T>(sql: string, params: any[] = []): Promise<T | null> {
        try {
            return await window.electronAPI.db.queryOne(sql, params);
        } catch (error) {
            console.error('SQLite queryOne error:', error);
            throw error;
        }
    }
}

// 导出一个单例实例
export const sqliteClient = new IpcSqliteClient();
