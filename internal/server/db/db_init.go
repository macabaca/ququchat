package db

import (
	"fmt"
	"net/url"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"ququchat/internal/config"
)

// DSN 构造适用于 GORM 的数据库连接串
func DSN(d config.Database) (string, error) {
	switch d.Driver {
	case "mysql":
		v := url.Values{}
		// 默认参数
		if _, ok := d.Params["parseTime"]; !ok {
			v.Set("parseTime", "true")
		}
		if _, ok := d.Params["loc"]; !ok {
			v.Set("loc", "Local")
		}
		if _, ok := d.Params["charset"]; !ok {
			v.Set("charset", "utf8mb4")
		}
		for k, val := range d.Params {
			v.Set(k, val)
		}
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s", d.User, d.Password, d.Host, d.Port, d.Name, v.Encode()), nil
	default:
		return "", fmt.Errorf("unsupported driver: %s", d.Driver)
	}
}

// OpenGorm 使用 GORM 打开数据库连接
func OpenGorm(d config.Database) (*gorm.DB, error) {
	dsn, err := DSN(d)
	if err != nil {
		return nil, err
	}
	switch d.Driver {
	case "mysql":
		return gorm.Open(mysql.Open(dsn), &gorm.Config{})
	default:
		return nil, fmt.Errorf("unsupported driver: %s", d.Driver)
	}
}
