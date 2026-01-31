package api

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"ququchat/internal/api/handler"
	"ququchat/internal/config"
	"ququchat/internal/middleware"
)

// SetupRouter 初始化 Gin 路由，并将数据库句柄注入到上下文中
func SetupRouter(db *gorm.DB, authCfg config.AuthSettings, chatCfg config.Chat) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	// 简单的 DB 注入中间件
	r.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Next()
	})

	// 静态资源：挂载 web 目录，便于本地验证前端页面
	r.Static("/web", "web")

	api := r.Group("/api")

	// 认证相关路由（注入认证配置）
	auth := handler.NewAuthHandler(db, authCfg)
	api.POST("/auth/register", auth.Register)
	api.POST("/auth/login", auth.Login)
	api.POST("/auth/refresh", auth.Refresh)
	api.POST("/auth/logout", middleware.JWTAuth(authCfg.JWTSecret), auth.Logout)

	userHandler := handler.NewUserHandler(db)
	friends := api.Group("/friends", middleware.JWTAuth(authCfg.JWTSecret))
	friends.POST("/add", userHandler.AddFriend)
	friends.POST("/remove", userHandler.RemoveFriend)
	friends.GET("/list", userHandler.ListFriends)
	friends.GET("/requests/incoming", userHandler.ListIncomingFriendRequests)
	friends.POST("/requests/respond", userHandler.RespondFriendRequest)

	groupHandler := handler.NewGroupHandler(db)
	groups := api.Group("/groups", middleware.JWTAuth(authCfg.JWTSecret))
	groups.POST("/create", groupHandler.CreateGroup)
	groups.GET("/:group_id", groupHandler.GetGroupDetail)
	groups.GET("/my", groupHandler.ListMyGroups)
	groups.POST("/:group_id/dismiss", groupHandler.DismissGroup)
	groups.POST("/:group_id/members/add", groupHandler.AddMembers)
	groups.POST("/:group_id/members/remove", groupHandler.RemoveMember)
	groups.POST("/:group_id/leave", groupHandler.LeaveGroup)
	groups.GET("/:group_id/members", groupHandler.ListGroupMembers)
	groups.POST("/:group_id/admins/add", groupHandler.AddAdmins)

	messageHandler := handler.NewMessageHandler(db, chatCfg.HistoryLimit)
	api.GET("/messages/history/before", middleware.JWTAuth(authCfg.JWTSecret), messageHandler.GetHistoryBefore)
	api.GET("/messages/history/after", middleware.JWTAuth(authCfg.JWTSecret), messageHandler.GetHistoryAfter)
	api.GET("/messages/history/latest", middleware.JWTAuth(authCfg.JWTSecret), messageHandler.GetLatestByFriend)
	api.GET("/messages/history/group", middleware.JWTAuth(authCfg.JWTSecret), messageHandler.GetLatestByGroup)

	wsHandler := handler.NewWsHandler(db)
	r.GET("/ws", middleware.JWTAuthFromHeaderOrQuery(authCfg.JWTSecret), wsHandler.Handle)

	return r
}
