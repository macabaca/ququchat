package main

import (
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

type Hub struct {
	clients    map[*Client]bool            //所有在线的客户端连接的集合
	userMap    map[string]map[*Client]bool //用户ID--->该用户的所有链接(支持多端连接)
	rooms      map[string]map[*Client]bool //房间ID--->该房间内的所有链接客户端
	register   chan *Client                //注册客户端的通道
	unregister chan *Client                //注销客户端的通道
	broadcast  chan Broadcast              //广播消息的通道
}

type Client struct {
	hub   *Hub            //当前客户端连接的hub
	conn  *websocket.Conn //当前客户端连接的websocket连接
	send  chan []byte     //消息发送队列，用于将发送到客户端的消息暂存
	user  string          //当前客户端连接的用户ID（UserID或用户名）
	rooms map[string]bool //当前客户端连接的用户所在的房间集合
}

type Message struct {
	Type    string `json:"type"`              //消息类型，比如：“join”、“leave”、“message”等
	Room    string `json:"room,omitempty"`    //消息所属的房间ID
	To      string `json:"to,omitempty"`      //消息接收者(UserID)，用于私聊
	Id      string `json:"id,omitempty"`      //消息ID，用于去重或确认
	Payload string `json:"payload,omitempty"` //消息内容，序列化的json数据，或者直接是text内容
	Seq     int64  `json:"seq,omitempty"`     //消息序列号，用于消息的顺序处理
	Time    int64  `json:"time,omitempty"`    //消息发送时间
}

type Broadcast struct {
	Room string `json:"room,omitempty"`
	Data []byte `json:"data,omitempty"`
	To   string `json:"to,omitempty"` //非空：表示私聊，如果为空，表示广播给房间的所有人
}

const (
	pongWait       = 60 * time.Second    //客户端在pingPeriod时间内没有响应，则认为连接断开
	pingPeriod     = (pongWait * 9) / 10 // 服务端主动向客户端发送的心跳检测周期
	writeWait      = 10 * time.Second    //消息发送超时时间
	maxMessageSize = 1024 * 8            //单条消息最大长度
)

func main() {
	//1. 获取客户端http请求
	//2. 验证token与origin域名的合法性
	//3. 升级http请求为websocket请求
	//4. 启动hub监听协程，对client进行注册等管理
	//5. 请求Reader()与Writter()监听协程
	fmt.Println("Hello, World!")
}
