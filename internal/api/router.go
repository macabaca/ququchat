package api

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"ququchat/internal/api/handler"
	"ququchat/internal/middleware"
)

// SetupRouter 初始化 Gin 路由，并将数据库句柄注入到上下文中
func SetupRouter(db *gorm.DB, jwtSecret string) *gin.Engine {
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

    // 路由分组
    api := r.Group("/api")

	// 认证相关路由
	auth := handler.NewAuthHandler(db, jwtSecret)
	api.POST("/auth/register", auth.Register)
	api.POST("/auth/login", auth.Login)
	api.POST("/auth/refresh", auth.Refresh)
	api.POST("/auth/logout", middleware.JWTAuth(jwtSecret), auth.Logout)
	// api.POST("/auth/logout-all", middleware.JWTAuth(jwtSecret), auth.LogoutAll)

	// 其他处理器可在后续实现
	// user := handler.NewUserHandler(db)
	// ws := handler.NewWsHandler(db)
	// api.Group("/user")
	// api.Group("/ws")

	return r
}
