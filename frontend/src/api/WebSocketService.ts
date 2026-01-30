import { BASE_URL } from "../configs/config";
import { Message } from "../types/models";

type MessageHandler = (message: Message) => void;
type StatusHandler = (isConnected: boolean) => void;

export class WebSocketService {
    private ws: WebSocket | null = null;
    private url: string;
    private token: string;
    private messageHandlers: MessageHandler[] = [];
    private statusHandlers: StatusHandler[] = [];
    
    // Heartbeat
    private pingInterval: NodeJS.Timeout | null = null;
    private pongTimeout: NodeJS.Timeout | null = null;
    private readonly PING_INTERVAL = 30000; // 30s
    private readonly PONG_TIMEOUT = 5000;   // 5s

    // Reconnection
    private reconnectTimeout: NodeJS.Timeout | null = null;
    private reconnectAttempts = 0;
    private readonly MAX_RECONNECT_DELAY = 30000;

    constructor(token: string) {
        this.token = token;
        // Convert HTTP Base URL to WS URL
        // e.g., https://api.com/api/v1 -> wss://api.com/ws
        // But docs say /ws is the endpoint. 
        // Assuming BASE_URL is like http://localhost:8080/api/v1 or http://localhost:8080/api
        // We need to construct ws://localhost:8080/ws?token=...
        
        // Let's assume for now we replace protocol and append /ws based on typical setups
        // Or we can parse BASE_URL. 
        // Let's try to derive it intelligently.
        const base = new URL(BASE_URL);
        const wsProtocol = base.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsBase = `${wsProtocol}//${base.host}`;
        this.url = `${wsBase}/ws?token=${encodeURIComponent(token)}`;
    }

    public connect() {
        if (this.ws?.readyState === WebSocket.OPEN) return;

        console.log('Connecting to WebSocket:', this.url);
        this.ws = new WebSocket(this.url);

        this.ws.onopen = () => {
            console.log('WebSocket Connected');
            this.reconnectAttempts = 0;
            this.notifyStatus(true);
            this.startHeartbeat();
        };

        this.ws.onmessage = (event) => {
            try {
                const data = JSON.parse(event.data);
                // Handle Heartbeat Pong if server sends one (though docs don't mention explicit pong format, 
                // usually we assume any message or specific pong resets timeout. 
                // Since docs don't specify server pong, we might just rely on connection staying open 
                // or send a ping and assume if write succeeds it's ok? 
                // Actually docs say "Service silently drops errors". 
                // Let's assume for now we just listen for messages.
                
                this.notifyMessage(data);
            } catch (e) {
                console.error('Failed to parse WS message', e);
            }
        };

        this.ws.onclose = () => {
            console.log('WebSocket Closed');
            this.notifyStatus(false);
            this.stopHeartbeat();
            this.scheduleReconnect();
        };

        this.ws.onerror = (error) => {
            console.error('WebSocket Error', error);
            // onClose will be called
        };
    }

    public disconnect() {
        this.stopHeartbeat();
        if (this.reconnectTimeout) {
            clearTimeout(this.reconnectTimeout);
            this.reconnectTimeout = null;
        }
        if (this.ws) {
            this.ws.close();
            this.ws = null;
        }
    }

    public sendMessage(message: any) {
        if (this.ws?.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify(message));
        } else {
            console.warn('WebSocket not connected, cannot send message');
            // Queueing could be implemented here
        }
    }

    public addMessageHandler(handler: MessageHandler) {
        this.messageHandlers.push(handler);
    }

    public removeMessageHandler(handler: MessageHandler) {
        this.messageHandlers = this.messageHandlers.filter(h => h !== handler);
    }

    public addStatusHandler(handler: StatusHandler) {
        this.statusHandlers.push(handler);
    }

    private notifyMessage(message: Message) {
        this.messageHandlers.forEach(handler => handler(message));
    }

    private notifyStatus(isConnected: boolean) {
        this.statusHandlers.forEach(handler => handler(isConnected));
    }

    private startHeartbeat() {
        this.stopHeartbeat();
        this.pingInterval = setInterval(() => {
            if (this.ws?.readyState === WebSocket.OPEN) {
                // Send a ping message if server supports it, or just a dummy message
                // Docs don't specify a ping format, but standard WS has ping frames.
                // Browser JS WebSocket API doesn't allow sending raw Ping frames.
                // We usually send a JSON ping. 
                // Since docs don't mention it, let's just send an empty object or specific type if needed.
                // For now, I'll assume connection presence is enough, or send a "ping" type if I could.
                // Re-reading docs: "Heartbeat mechanism... if not in docs, implement it".
                // So I will send a custom ping.
                this.ws.send(JSON.stringify({ type: 'ping', timestamp: Date.now() }));
            }
        }, this.PING_INTERVAL);
    }

    private stopHeartbeat() {
        if (this.pingInterval) {
            clearInterval(this.pingInterval);
            this.pingInterval = null;
        }
    }

    private scheduleReconnect() {
        if (this.reconnectTimeout) return;

        const delay = Math.min(1000 * Math.pow(2, this.reconnectAttempts), this.MAX_RECONNECT_DELAY);
        console.log(`Reconnecting in ${delay}ms...`);
        
        this.reconnectTimeout = setTimeout(() => {
            this.reconnectAttempts++;
            this.reconnectTimeout = null;
            this.connect();
        }, delay);
    }
}
