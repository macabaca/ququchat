
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

