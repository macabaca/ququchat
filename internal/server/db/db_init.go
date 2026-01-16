package db

import (
	"fmt"
	"net/url"
	"strings"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"ququchat/internal/config"
)

// DSN 构造适用于 GORM 的数据库连接串
func DSN(d config.Database) (string, error) {
	switch d.Driver {
	case "mysql":
		v := url.Values{}
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
	case "postgres":
		parts := []string{
			fmt.Sprintf("host=%s", d.Host),
			fmt.Sprintf("port=%d", d.Port),
			fmt.Sprintf("user=%s", d.User),
			fmt.Sprintf("password=%s", d.Password),
			fmt.Sprintf("dbname=%s", d.Name),
		}
		if d.Params == nil {
			d.Params = map[string]string{}
		}
		if _, ok := d.Params["sslmode"]; !ok {
			d.Params["sslmode"] = "disable"
		}
		for k, val := range d.Params {
			parts = append(parts, fmt.Sprintf("%s=%s", k, val))
		}
		return strings.Join(parts, " "), nil
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
	case "postgres":
		return gorm.Open(postgres.Open(dsn), &gorm.Config{})
	default:
		return nil, fmt.Errorf("unsupported driver: %s", d.Driver)
	}
}
