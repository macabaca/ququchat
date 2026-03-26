package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"ququchat/internal/config"
)

type generateResponse struct {
	Images []struct {
		URL string `json:"url"`
	} `json:"images"`
	Message string `json:"message"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func main() {
	prompt := flag.String("prompt", "一只白色小猫，写实风格", "文生图提示词")
	imageSize := flag.String("size", "1024x1024", "图片尺寸")
	batchSize := flag.Int("batch", 1, "生成张数")
	steps := flag.Int("steps", 20, "推理步数")
	guidance := flag.Float64("guidance", 7.5, "guidance scale")
	flag.Parse()

	cfg, err := config.LoadDefault()
	if err != nil {
		log.Fatalf("读取配置失败: %v", err)
	}
	if strings.TrimSpace(cfg.AIGC.APIKey) == "" {
		log.Fatalf("aigc.api_key 为空")
	}
	if strings.TrimSpace(cfg.AIGC.BaseURL) == "" {
		log.Fatalf("aigc.base_url 为空")
	}
	if strings.TrimSpace(cfg.AIGC.Model) == "" {
		log.Fatalf("aigc.model 为空")
	}

	reqBody := map[string]interface{}{
		"model":               strings.TrimSpace(cfg.AIGC.Model),
		"prompt":              strings.TrimSpace(*prompt),
		"image_size":          strings.TrimSpace(*imageSize),
		"batch_size":          *batchSize,
		"num_inference_steps": *steps,
		"guidance_scale":      *guidance,
	}
	rawReq, err := json.Marshal(reqBody)
	if err != nil {
		log.Fatalf("序列化请求失败: %v", err)
	}

	base := strings.TrimRight(strings.TrimSpace(cfg.AIGC.BaseURL), "/")
	httpReq, err := http.NewRequest(http.MethodPost, base+"/images/generations", bytes.NewReader(rawReq))
	if err != nil {
		log.Fatalf("构造请求失败: %v", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.AIGC.APIKey))
	httpReq.Header.Set("Content-Type", "application/json")

	cli := &http.Client{Timeout: 2 * time.Minute}
	httpResp, err := cli.Do(httpReq)
	if err != nil {
		log.Fatalf("调用文生图接口失败: %v", err)
	}
	defer httpResp.Body.Close()

	rawResp, err := io.ReadAll(httpResp.Body)
	if err != nil {
		log.Fatalf("读取接口响应失败: %v", err)
	}
	var out generateResponse
	if err := json.Unmarshal(rawResp, &out); err != nil {
		log.Fatalf("解析接口响应失败: %v, raw=%s", err, string(rawResp))
	}
	if httpResp.StatusCode >= 400 {
		if out.Error != nil && strings.TrimSpace(out.Error.Message) != "" {
			log.Fatalf("文生图接口失败: %s", strings.TrimSpace(out.Error.Message))
		}
		if strings.TrimSpace(out.Message) != "" {
			log.Fatalf("文生图接口失败: %s", strings.TrimSpace(out.Message))
		}
		log.Fatalf("文生图接口失败: http=%d raw=%s", httpResp.StatusCode, string(rawResp))
	}
	if len(out.Images) == 0 {
		log.Fatalf("接口未返回图片 URL: raw=%s", string(rawResp))
	}

	fmt.Printf("返回图片数量: %d\n", len(out.Images))
	for i, image := range out.Images {
		imgURL := strings.TrimSpace(image.URL)
		if imgURL == "" {
			fmt.Printf("[%d] URL 为空\n", i)
			continue
		}
		info, err := probeURL(cli, imgURL)
		if err != nil {
			fmt.Printf("[%d] URL=%s 探测失败: %v\n", i, imgURL, err)
			continue
		}
		fmt.Printf("[%d] URL=%s\n", i, imgURL)
		fmt.Printf("    header_content_type=%s\n", info.HeaderContentType)
		fmt.Printf("    detect_content_type=%s\n", info.DetectedContentType)
		fmt.Printf("    magic_format=%s\n", info.MagicFormat)
		fmt.Printf("    path_ext=%s\n", info.PathExt)
		fmt.Printf("    content_length=%d\n", info.ContentLength)
	}
}

type probeInfo struct {
	HeaderContentType   string
	DetectedContentType string
	MagicFormat         string
	PathExt             string
	ContentLength       int64
}

func probeURL(cli *http.Client, raw string) (*probeInfo, error) {
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http=%d", resp.StatusCode)
	}
	head := make([]byte, 512)
	n, readErr := io.ReadFull(resp.Body, head)
	if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
		return nil, readErr
	}
	head = head[:n]
	u, _ := url.Parse(raw)
	return &probeInfo{
		HeaderContentType:   strings.TrimSpace(resp.Header.Get("Content-Type")),
		DetectedContentType: http.DetectContentType(head),
		MagicFormat:         detectMagic(head),
		PathExt:             strings.ToLower(path.Ext(u.Path)),
		ContentLength:       resp.ContentLength,
	}, nil
}

func detectMagic(b []byte) string {
	if len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF {
		return "jpeg"
	}
	if len(b) >= 8 && bytes.Equal(b[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}) {
		return "png"
	}
	if len(b) >= 6 && (bytes.Equal(b[:6], []byte("GIF87a")) || bytes.Equal(b[:6], []byte("GIF89a"))) {
		return "gif"
	}
	if len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")) {
		return "webp"
	}
	return "unknown"
}
