import Dexie, { Table } from 'dexie';
import { Message, Room, User, FriendShip, RoomMember } from '../types/models'

// 定义本地数据库
export class LocalDatabase extends Dexie {
    public messages!: Table<Message, string>;
    public rooms!: Table<Room, string>;
    public users!: Table<User, number>;
    public friendships!: Table<FriendShip, string>;
    // roomMembers 使用复合主键
    public roomMembers!: Table<RoomMember, [string, string]>;

    constructor() {
        super('ChatDatabase');
        this.version(1).stores({
            // 定义表结构和索引
            messages: 'id, roomID, senderID, parentMessageID, createdAt',
            rooms: 'id, roomType',
            users: 'id, username',
            friendships: 'id, &[userIDA+useriDB]',
            roomMembers: '[roomID+userID], roomID, userID'
        });
    }
}

export const db = new LocalDatabase();