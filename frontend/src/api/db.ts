import Dexie, { Table } from 'dexie';
import { Message, User } from '../types/models'

type RoomRecord = {
    id: string;
    roomType: string;
};

type FriendShipRecord = {
    id: string;
    userIDA: string;
    useriDB: string;
};

type RoomMemberRecord = {
    roomID: string;
    userID: string;
};

// 定义本地数据库
export class LocalDatabase extends Dexie {
    public messages!: Table<Message, string>;
    public rooms!: Table<RoomRecord, string>;
    public users!: Table<User, number>;
    public friendships!: Table<FriendShipRecord, string>;
    public roomMembers!: Table<RoomMemberRecord, [string, string]>;

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