import { Message } from "./models";

// 客户端 -> 服务器
export interface WsClientMsg_SendMessage {
    type: "send_message";
    payload: {
        room_id: number;
        content: string;
        is_image: boolean;
    };
}

export interface WsClientMsg_FetchHistory {
    type: "fetch_history";
    payload: {
        room_id: number;
        page: number;
        page_size: number;
    };
}

export type WsServerEventType = 'friend_list_changed' | 'group_list_changed' | 'friend_request_changed' | 'friend_request_created' | 'friend_request_accepted' | 'friend_list_updated' | 'group_member_added' | 'group_member_removed' | 'group_list_updated';

export interface WsServerEvent {
    type: 'system_event';
    event: WsServerEventType;
}

export interface WsServerHeartbeat {
    type: 'ping' | 'pong';
    timestamp?: number;
    ts?: number;
}

export type WsServerMsg = Message | WsServerEvent | WsServerHeartbeat;

