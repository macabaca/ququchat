import { BASE_URL } from "../configs/config";
import { Message } from "../types/models";
import { WsServerEvent, WsServerHeartbeat, WsServerMsg } from "../types/websocket";

type MessageHandler = (message: Message) => void;
type StatusHandler = (isConnected: boolean) => void;
type ReconnectHandler = (isReconnecting: boolean) => void;
type SystemEventHandler = (event: WsServerEvent) => void;

export class WebSocketService {
    private ws: WebSocket | null = null;
    private url: string;
    private messageHandlers: MessageHandler[] = [];
    private statusHandlers: StatusHandler[] = [];
    private reconnectHandlers: ReconnectHandler[] = [];
    private systemEventHandlers: SystemEventHandler[] = [];
    
    private pingInterval: NodeJS.Timeout | null = null;
    private pongTimeout: NodeJS.Timeout | null = null;
    private readonly PING_INTERVAL = 30000;
    private readonly PONG_TIMEOUT = 10000;

    private reconnectTimeout: NodeJS.Timeout | null = null;
    private reconnectAttempts = 0;
    private readonly MAX_RECONNECT_DELAY = 30000;
    private shouldReconnect = true;

    private buildUrl(token: string) {
        const base = new URL(BASE_URL);
        const wsProtocol = base.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsBase = `${wsProtocol}//${base.host}`;
        return `${wsBase}/ws?token=${encodeURIComponent(token)}`;
    }

    constructor(token: string) {
        // Convert HTTP Base URL to WS URL
        // e.g., https://api.com/api/v1 -> wss://api.com/ws
        // But docs say /ws is the endpoint. 
        // Assuming BASE_URL is like http://localhost:8080/api/v1 or http://localhost:8080/api
        // We need to construct ws://localhost:8080/ws?token=...
        
        // Let's assume for now we replace protocol and append /ws based on typical setups
        // Or we can parse BASE_URL. 
        // Let's try to derive it intelligently.
        this.url = this.buildUrl(token);
    }

    public connect() {
        this.shouldReconnect = true;
        if (this.ws?.readyState === WebSocket.OPEN || this.ws?.readyState === WebSocket.CONNECTING) return;

        console.log('Connecting to WebSocket:', this.url);
        this.ws = new WebSocket(this.url);

        this.ws.onopen = () => {
            console.log('WebSocket Connected');
            this.reconnectAttempts = 0;
            this.notifyReconnecting(false);
            this.notifyStatus(true);
            this.startHeartbeat();
        };

        this.ws.onmessage = (event) => {
            this.markPongReceived();
            try {
                const data = JSON.parse(event.data) as WsServerMsg;
                if ((data as WsServerHeartbeat)?.type === 'pong') return;
                if ((data as WsServerHeartbeat)?.type === 'ping') {
                    this.ws?.send(JSON.stringify({ type: 'pong', timestamp: Date.now() }));
                    return;
                }
                if ((data as WsServerEvent)?.type === 'system_event') {
                    this.notifySystemEvent(data as WsServerEvent);
                    return;
                }
                this.notifyMessage(data as Message);
            } catch (e) {
                console.error('Failed to parse WS message', e);
            }
        };

        this.ws.onclose = (event) => {
            console.log('WebSocket Closed', event.code, event.reason, event.wasClean);
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
        this.shouldReconnect = false;
        this.notifyReconnecting(false);
        this.notifyStatus(false);
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

    public updateToken(token: string) {
        const nextUrl = this.buildUrl(token);
        if (this.url === nextUrl) return;
        this.url = nextUrl;
        if (this.ws) {
            if (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING) {
                this.ws.close();
            } else if (this.ws.readyState === WebSocket.CLOSED) {
                this.connect();
            }
        } else if (this.shouldReconnect) {
            this.connect();
        }
    }

    public sendMessage(message: any) {
        if (this.ws?.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify(message));
        } else {
            console.warn('WebSocket not connected, cannot send message');
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

    public addReconnectHandler(handler: ReconnectHandler) {
        this.reconnectHandlers.push(handler);
    }

    public addSystemEventHandler(handler: SystemEventHandler) {
        this.systemEventHandlers.push(handler);
    }

    public removeSystemEventHandler(handler: SystemEventHandler) {
        this.systemEventHandlers = this.systemEventHandlers.filter(h => h !== handler);
    }

    private notifyMessage(message: Message) {
        this.messageHandlers.forEach(handler => handler(message));
    }

    private notifySystemEvent(event: WsServerEvent) {
        this.systemEventHandlers.forEach(handler => handler(event));
    }

    private notifyStatus(isConnected: boolean) {
        this.statusHandlers.forEach(handler => handler(isConnected));
    }

    private notifyReconnecting(isReconnecting: boolean) {
        this.reconnectHandlers.forEach(handler => handler(isReconnecting));
    }

    private startHeartbeat() {
        this.stopHeartbeat();
        this.pingInterval = setInterval(() => {
            this.sendPing();
        }, this.PING_INTERVAL);
        this.sendPing();
    }

    private sendPing() {
        if (this.ws?.readyState !== WebSocket.OPEN) return;
        this.ws.send(JSON.stringify({ type: 'ping', timestamp: Date.now() }));
        this.schedulePongTimeout();
    }

    private schedulePongTimeout() {
        if (this.pongTimeout) {
            clearTimeout(this.pongTimeout);
        }
        this.pongTimeout = setTimeout(() => {
            if (this.ws?.readyState === WebSocket.OPEN) {
                console.warn('WebSocket pong timeout');
                this.ws.close();
            }
        }, this.PONG_TIMEOUT);
    }

    private markPongReceived() {
        if (this.pongTimeout) {
            clearTimeout(this.pongTimeout);
            this.pongTimeout = null;
        }
    }

    private stopHeartbeat() {
        if (this.pingInterval) {
            clearInterval(this.pingInterval);
            this.pingInterval = null;
        }
        if (this.pongTimeout) {
            clearTimeout(this.pongTimeout);
            this.pongTimeout = null;
        }
    }

    private scheduleReconnect() {
        if (!this.shouldReconnect) return;
        if (this.reconnectTimeout) return;

        const delay = Math.min(1000 * Math.pow(2, this.reconnectAttempts), this.MAX_RECONNECT_DELAY);
        console.log(`Reconnecting in ${delay}ms...`);
        this.notifyReconnecting(true);

        this.reconnectTimeout = setTimeout(() => {
            this.reconnectAttempts++;
            this.reconnectTimeout = null;
            console.log('[WS][entry:reconnect] scheduleReconnect -> connect');
            this.connect();
        }, delay);
    }
}
