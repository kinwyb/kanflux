package wxcom

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

// FileDownloader 文件下载器
type FileDownloader struct {
	client  *http.Client
	timeout time.Duration
}

// NewFileDownloader 创建文件下载器
func NewFileDownloader(timeout time.Duration) *FileDownloader {
	return &FileDownloader{
		client: &http.Client{
			Timeout: timeout,
		},
		timeout: timeout,
	}
}

// DownloadFile 下载文件并解密
// url: 文件下载地址
// aesKey: Base64编码的AES-256密钥 (来自消息中的 image.aeskey 或 file.aeskey)
// 返回: 解密后的文件数据, 文件名, 错误
func (d *FileDownloader) DownloadFile(ctx context.Context, fileURL, aesKey string) ([]byte, string, error) {
	if fileURL == "" {
		return nil, "", fmt.Errorf("file URL is empty")
	}

	// 下载加密文件
	encryptedData, filename, err := d.downloadRaw(ctx, fileURL)
	if err != nil {
		return nil, "", fmt.Errorf("download failed: %w", err)
	}

	// 如果没有 aesKey，直接返回原始数据
	if aesKey == "" {
		return encryptedData, filename, nil
	}

	// AES-256-CBC 解密
	decryptedData, err := decryptAES256CBC(encryptedData, aesKey)
	if err != nil {
		return nil, "", fmt.Errorf("decryption failed: %w", err)
	}

	return decryptedData, filename, nil
}

// downloadRaw 下载原始加密文件
func (d *FileDownloader) downloadRaw(ctx context.Context, fileURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, "", err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	// 读取文件数据
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	// 从 Content-Disposition 解析文件名
	filename := parseFilenameFromDisposition(resp.Header.Get("Content-Disposition"))
	if filename == "" {
		// 从 URL 解析文件名
		filename = parseFilenameFromURL(fileURL)
	}

	return data, filename, nil
}

// decryptAES256CBC 使用 AES-256-CBC 解密数据
// aesKey: Base64编码的AES密钥
// IV取密钥的前16字节
func decryptAES256CBC(encryptedData []byte, aesKey string) ([]byte, error) {
	if len(encryptedData) == 0 {
		return nil, fmt.Errorf("encrypted data is empty")
	}

	if aesKey == "" {
		return nil, fmt.Errorf("aes key is empty")
	}

	// Base64解码密钥 (补齐padding)
	paddedKey := aesKey
	if mod := len(aesKey) % 4; mod != 0 {
		paddedKey = aesKey + strings.Repeat("=", 4-mod)
	}

	key, err := base64.StdEncoding.DecodeString(paddedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode aes key: %w", err)
	}

	// AES-256 需要 32 字节密钥
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid key length: %d (expected 32)", len(key))
	}

	// IV 取密钥的前 16 字节
	iv := key[:16]

	// 创建 AES 解密器
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// 确保数据长度是块大小的倍数
	blockSize := block.BlockSize()
	if len(encryptedData)%blockSize != 0 {
		// 补零对齐
		paddedData := make([]byte, ((len(encryptedData)/blockSize)+1)*blockSize)
		copy(paddedData, encryptedData)
		encryptedData = paddedData
	}

	// CBC 解密
	mode := cipher.NewCBCDecrypter(block, iv)
	decrypted := make([]byte, len(encryptedData))
	mode.CryptBlocks(decrypted, encryptedData)

	// 去除 PKCS#7 填充
	decrypted, err = removePKCS7Padding(decrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to remove padding: %w", err)
	}

	return decrypted, nil
}

// removePKCS7Padding 去除 PKCS#7 填充
func removePKCS7Padding(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data is empty")
	}

	// 获取填充长度 (最后一个字节)
	padLen := int(data[len(data)-1])

	// 验证填充长度
	if padLen < 1 || padLen > 32 || padLen > len(data) {
		return nil, fmt.Errorf("invalid padding length: %d", padLen)
	}

	// 验证所有填充字节是否一致
	for i := len(data) - padLen; i < len(data); i++ {
		if int(data[i]) != padLen {
			return nil, fmt.Errorf("invalid padding bytes")
		}
	}

	return data[:len(data)-padLen], nil
}

// parseFilenameFromDisposition 从 Content-Disposition 头解析文件名
func parseFilenameFromDisposition(disposition string) string {
	if disposition == "" {
		return ""
	}

	// 尝试匹配 filename*=UTF-8''xxx 格式 (RFC 5987)
	utf8Regex := regexp.MustCompile(`filename\*=UTF-8''([^;\s]+)`)
	if matches := utf8Regex.FindStringSubmatch(disposition); len(matches) > 1 {
		// URL解码文件名
		decoded, err := url.QueryUnescape(matches[1])
		if err == nil {
			return decoded
		}
		return matches[1]
	}

	// 尝试匹配 filename="xxx" 或 filename=xxx 格式
	filenameRegex := regexp.MustCompile(`filename="?([^";\s]+)"?`)
	if matches := filenameRegex.FindStringSubmatch(disposition); len(matches) > 1 {
		// URL解码文件名
		decoded, err := url.QueryUnescape(matches[1])
		if err == nil {
			return decoded
		}
		return matches[1]
	}

	return ""
}

// parseFilenameFromURL 从URL解析文件名
func parseFilenameFromURL(fileURL string) string {
	u, err := url.Parse(fileURL)
	if err != nil {
		return ""
	}

	filename := path.Base(u.Path)
	if filename == "" || filename == "." || filename == "/" {
		// 使用时间戳生成文件名
		filename = fmt.Sprintf("file_%d", time.Now().UnixMilli())
	}

	return filename
}

// DecryptFileData 直接解密文件数据 (不需要下载)
// 用于已经下载了数据但需要解密的场景
func DecryptFileData(encryptedData []byte, aesKey string) ([]byte, error) {
	return decryptAES256CBC(encryptedData, aesKey)
}

// DownloadAndDecrypt 下载并解密文件的便捷方法
// 可以直接使用 WxComChannel 调用
func DownloadAndDecrypt(ctx context.Context, fileURL, aesKey string, timeout time.Duration) ([]byte, string, error) {
	downloader := NewFileDownloader(timeout)
	return downloader.DownloadFile(ctx, fileURL, aesKey)
}

// SaveFile 保存文件数据到指定路径
func SaveFile(data []byte, filepath string) error {
	// 这里只返回数据，实际保存由调用者处理
	// 在实际应用中可以使用 os.WriteFile
	return nil
}

// DetectMimeType 从文件数据检测 MIME 类型
func DetectMimeType(data []byte) string {
	// 常见文件类型的魔数检测
	if len(data) < 4 {
		return "application/octet-stream"
	}

	// PNG
	if bytes.HasPrefix(data, []byte{0x89, 0x50, 0x4E, 0x47}) {
		return "image/png"
	}

	// JPEG
	if bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}) {
		return "image/jpeg"
	}

	// GIF
	if bytes.HasPrefix(data, []byte{0x47, 0x49, 0x46}) {
		return "image/gif"
	}

	// PDF
	if bytes.HasPrefix(data, []byte{0x25, 0x50, 0x44, 0x46}) {
		return "application/pdf"
	}

	// ZIP (包括 Office 文档)
	if bytes.HasPrefix(data, []byte{0x50, 0x4B, 0x03, 0x04}) {
		return "application/zip"
	}

	return "application/octet-stream"
}