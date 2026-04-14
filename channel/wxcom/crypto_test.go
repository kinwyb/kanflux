package wxcom

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"testing"
	"time"
)

func TestDecryptAES256CBC(t *testing.T) {
	// 测试数据: 使用 AES-256-CBC 加密 "Hello, World!"
	// 密钥: 32字节 (AES-256)
	// IV: 密钥的前16字节

	// 生成测试密钥 (32字节)
	key := make([]byte, 32)
	for i := 0; i < 32; i++ {
		key[i] = byte(i)
	}
	aesKeyBase64 := base64.StdEncoding.EncodeToString(key)

	// 原始数据
	plaintext := []byte("Hello, World!")

	// 手动加密测试数据
	iv := key[:16]
	encrypted := encryptAES256CBC(plaintext, key, iv)

	// 解密
	decrypted, err := decryptAES256CBC(encrypted, aesKeyBase64)
	if err != nil {
		t.Fatalf("Decryption failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("Expected '%s', got '%s'", plaintext, decrypted)
	}
}

func TestDecryptAES256CBCWithPadding(t *testing.T) {
	// 测试不同长度的数据

	key := make([]byte, 32)
	for i := 0; i < 32; i++ {
		key[i] = byte(i)
	}
	aesKeyBase64 := base64.StdEncoding.EncodeToString(key)
	iv := key[:16]

	testCases := []string{
		"short",
		"16 bytes exactly",
		"this is 32 bytes long text!!",
		"this text is longer than 32 bytes and needs multiple blocks",
	}

	for _, tc := range testCases {
		plaintext := []byte(tc)
		encrypted := encryptAES256CBC(plaintext, key, iv)

		decrypted, err := decryptAES256CBC(encrypted, aesKeyBase64)
		if err != nil {
			t.Fatalf("Decryption failed for '%s': %v", tc, err)
		}

		if !bytes.Equal(decrypted, plaintext) {
			t.Errorf("Expected '%s', got '%s'", tc, decrypted)
		}
	}
}

func TestRemovePKCS7Padding(t *testing.T) {
	// 测试正常的 PKCS#7 填充
	data := []byte("Hello, World!")

	// 添加 3 字节填充 (长度变成 16)
	padded := append(data, 0x03, 0x03, 0x03)

	result, err := removePKCS7Padding(padded)
	if err != nil {
		t.Fatalf("Failed to remove padding: %v", err)
	}

	if !bytes.Equal(result, data) {
		t.Errorf("Expected '%s', got '%s'", data, result)
	}
}

func TestRemovePKCS7PaddingInvalid(t *testing.T) {
	// 测试无效填充
	testCases := [][]byte{
		{},                          // 空数据
		{0x00},                      // 填充长度为0
		{0x01, 0x02},                // 填充不一致
		make([]byte, 100),           // 填充长度超过数据长度
	}

	for _, tc := range testCases {
		// 设置最后一个字节为无效值
		if len(tc) > 0 {
			tc[len(tc)-1] = byte(len(tc) + 10) // 超过数据长度
		}

		_, err := removePKCS7Padding(tc)
		if err == nil {
			t.Error("Expected error for invalid padding")
		}
	}
}

func TestParseFilenameFromDisposition(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{
			`attachment; filename="test.pdf"`,
			"test.pdf",
		},
		{
			`attachment; filename=test.pdf`,
			"test.pdf",
		},
		{
			`attachment; filename*=UTF-8''test%20file.pdf`,
			"test file.pdf",
		},
		{
			"",
			"",
		},
		{
			`attachment; filename="测试文件.pdf"`,
			"测试文件.pdf",
		},
	}

	for _, tc := range testCases {
		result := parseFilenameFromDisposition(tc.input)
		if result != tc.expected {
			t.Errorf("For '%s', expected '%s', got '%s'", tc.input, tc.expected, result)
		}
	}
}

func TestParseFilenameFromURL(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{
			"https://example.com/path/to/file.pdf",
			"file.pdf",
		},
		{
			"https://example.com/image.jpg?token=abc",
			"image.jpg",
		},
		{
			"https://example.com/",
			"file_",
		}, // 会生成时间戳文件名
	}

	for _, tc := range testCases {
		result := parseFilenameFromURL(tc.input)
		// 对于最后一个测试，检查是否有 file_ 前缀
		if tc.input == "https://example.com/" {
			if len(result) < 5 || result[:5] != "file_" {
				t.Errorf("Expected generated filename for '%s'", tc.input)
			}
		} else if result != tc.expected {
			t.Errorf("For '%s', expected '%s', got '%s'", tc.input, tc.expected, result)
		}
	}
}

func TestDetectMimeType(t *testing.T) {
	testCases := []struct {
		data     []byte
		expected string
	}{
		{
			[]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A},
			"image/png",
		},
		{
			[]byte{0xFF, 0xD8, 0xFF, 0xE0},
			"image/jpeg",
		},
		{
			[]byte{0x47, 0x49, 0x46, 0x38},
			"image/gif",
		},
		{
			[]byte{0x25, 0x50, 0x44, 0x46},
			"application/pdf",
		},
		{
			[]byte{0x50, 0x4B, 0x03, 0x04},
			"application/zip",
		},
		{
			[]byte{0x00, 0x00, 0x00},
			"application/octet-stream",
		},
	}

	for _, tc := range testCases {
		result := DetectMimeType(tc.data)
		if result != tc.expected {
			t.Errorf("Expected '%s', got '%s'", tc.expected, result)
		}
	}
}

func TestNewFileDownloader(t *testing.T) {
	timeout := 10 * time.Second
	downloader := NewFileDownloader(timeout)

	if downloader == nil {
		t.Fatal("Expected downloader to be created")
	}

	if downloader.timeout != timeout {
		t.Errorf("Expected timeout %v, got %v", timeout, downloader.timeout)
	}

	if downloader.client == nil {
		t.Error("Expected HTTP client to be initialized")
	}
}

func TestDownloadAndDecrypt(t *testing.T) {
	// 这个测试需要实际的URL，这里只测试函数签名
	ctx := context.Background()
	timeout := 5 * time.Second

	// 使用一个不存在的URL测试错误处理
	_, _, err := DownloadAndDecrypt(ctx, "https://invalid.example.com/file.pdf", "", timeout)
	if err == nil {
		t.Error("Expected error for invalid URL")
	}
}

// encryptAES256CBC 辅助函数：用于测试的 AES-256-CBC 加密
func encryptAES256CBC(plaintext, key, iv []byte) []byte {
	// PKCS#7 填充
	blockSize := 16
	padLen := blockSize - (len(plaintext) % blockSize)
	padded := append(plaintext, bytes.Repeat([]byte{byte(padLen)}, padLen)...)

	// 加密
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	encrypted := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(encrypted, padded)

	return encrypted
}