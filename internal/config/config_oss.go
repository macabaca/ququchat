package config

type OSS struct {
	Region          string `yaml:"region" json:"region"`
	Endpoint        string `yaml:"endpoint" json:"endpoint"`
	AccessKeyID     string `yaml:"access_key_id" json:"access_key_id"`
	AccessKeySecret string `yaml:"access_key_secret" json:"access_key_secret"`
	SecurityToken   string `yaml:"security_token" json:"security_token"`
	Bucket          string `yaml:"bucket" json:"bucket"`
	DisableSSL      bool   `yaml:"disable_ssl" json:"disable_ssl"`
	UseCName        bool   `yaml:"use_cname" json:"use_cname"`
	UsePathStyle    bool   `yaml:"use_path_style" json:"use_path_style"`
}

