package crypto

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// validKey — 32-байтовый ключ для тестов (произвольные байты).
func validKey() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i + 1)
	}
	return k
}

// TestNewCipherRejectsBadKeyLength — ключ ≠ 32 байта запрещён (AES-256).
func TestNewCipherRejectsBadKeyLength(t *testing.T) {
	cases := []struct {
		name string
		key  []byte
	}{
		{"пустой", []byte{}},
		{"16 байт (AES-128)", make([]byte, 16)},
		{"24 байта (AES-192)", make([]byte, 24)},
		{"31 байт (короткий)", make([]byte, 31)},
		{"33 байта (длинный)", make([]byte, 33)},
		{"64 байта (двойной)", make([]byte, 64)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewCipher(tc.key)
			if c != nil {
				t.Errorf("NewCipher вернул не-nil cipher для невалидного ключа")
			}
			if !errors.Is(err, ErrInvalidKeyLength) {
				t.Errorf("ожидалась ErrInvalidKeyLength, получено: %v", err)
			}
		})
	}
}

// TestNewCipherAcceptsValidKey — 32 байта принимаются.
func TestNewCipherAcceptsValidKey(t *testing.T) {
	c, err := NewCipher(validKey())
	if err != nil {
		t.Fatalf("NewCipher(32 байта): %v", err)
	}
	if c == nil {
		t.Fatal("NewCipher вернул nil без ошибки")
	}
}

// TestEncryptDecryptRoundTrip — базовый round-trip с AAD.
func TestEncryptDecryptRoundTrip(t *testing.T) {
	c, _ := NewCipher(validKey())
	plain := []byte("Иванов Иван Петрович")
	aad := []byte("session-uuid + pseudonym")

	ct, err := c.Encrypt(plain, aad)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Equal(ct, plain) {
		t.Error("ciphertext совпадает с plaintext — шифрование не сработало")
	}

	decrypted, err := c.Decrypt(ct, aad)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(decrypted, plain) {
		t.Errorf("decrypted=%q, ожидалось %q", decrypted, plain)
	}
}

// TestDecryptWithDifferentAADFails — AAD при Decrypt должен совпадать
// с AAD при Encrypt; иначе ошибка аутентификации.
func TestDecryptWithDifferentAADFails(t *testing.T) {
	c, _ := NewCipher(validKey())
	plain := []byte("секретное значение")
	ct, err := c.Encrypt(plain, []byte("aad-1"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	_, err = c.Decrypt(ct, []byte("aad-2"))
	if err == nil {
		t.Fatal("Decrypt с другим AAD должен падать (защита от swap)")
	}
	// raw-значение в сообщении ошибки появляться не должно
	if strings.Contains(err.Error(), string(plain)) {
		t.Error("сообщение ошибки содержит plaintext (инвариант raw-в-логах)")
	}
}

// TestDecryptTamperedCiphertextFails — изменение байта ciphertext
// должно ловиться GCM-tag'ом.
func TestDecryptTamperedCiphertextFails(t *testing.T) {
	c, _ := NewCipher(validKey())
	plain := []byte("Договор 12-345/2026")
	aad := []byte("aad")
	ct, err := c.Encrypt(plain, aad)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// Поломаем последний байт (тег GCM).
	ct[len(ct)-1] ^= 0xFF
	_, err = c.Decrypt(ct, aad)
	if err == nil {
		t.Error("Decrypt поломанного ciphertext должен падать (GCM auth)")
	}
}

// TestDecryptShortCiphertextFails — слишком короткие данные явно
// невалидны (короче nonce+tag).
func TestDecryptShortCiphertextFails(t *testing.T) {
	c, _ := NewCipher(validKey())
	_, err := c.Decrypt([]byte{1, 2, 3}, nil)
	if !errors.Is(err, ErrCiphertextTooShort) {
		t.Errorf("ожидалась ErrCiphertextTooShort, получено: %v", err)
	}
}

// TestEncryptUniqueCiphertexts — Encrypt одного plaintext с одним AAD
// должен возвращать РАЗНЫЕ ciphertext'ы (свежий nonce каждый раз).
func TestEncryptUniqueCiphertexts(t *testing.T) {
	c, _ := NewCipher(validKey())
	plain := []byte("одно и то же")
	aad := []byte("один и тот же aad")

	ct1, err := c.Encrypt(plain, aad)
	if err != nil {
		t.Fatalf("Encrypt #1: %v", err)
	}
	ct2, err := c.Encrypt(plain, aad)
	if err != nil {
		t.Fatalf("Encrypt #2: %v", err)
	}
	if bytes.Equal(ct1, ct2) {
		t.Error("два Encrypt одного plaintext дали один ciphertext — nonce не свежий")
	}
	// Но оба должны быть расшифровываемыми
	for i, ct := range [][]byte{ct1, ct2} {
		dec, err := c.Decrypt(ct, aad)
		if err != nil {
			t.Errorf("Decrypt ciphertext #%d: %v", i+1, err)
		}
		if !bytes.Equal(dec, plain) {
			t.Errorf("Decrypt ciphertext #%d вернул %q, ожидалось %q",
				i+1, dec, plain)
		}
	}
}

// TestEncryptDeterministicWithFixedRand — с инжектированным источником
// случайности результат становится детерминированным (для отладки).
func TestEncryptDeterministicWithFixedRand(t *testing.T) {
	fixed := bytes.NewReader(bytes.Repeat([]byte{0xAB}, 64))
	c, err := NewCipherWithRand(validKey(), fixed)
	if err != nil {
		t.Fatalf("NewCipherWithRand: %v", err)
	}
	plain := []byte("test")
	ct, err := c.Encrypt(plain, []byte("aad"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// Формат: nonce(12) || ct || tag(16). nonce — первые 12 байт.
	wantNonce := bytes.Repeat([]byte{0xAB}, 12)
	if !bytes.Equal(ct[:12], wantNonce) {
		t.Errorf("nonce из инжектированного reader не подхвачен: %x", ct[:12])
	}
}

// TestNewCipherWithRandRejectsBadKey — длина ключа проверяется и
// в NewCipherWithRand (DRY).
func TestNewCipherWithRandRejectsBadKey(t *testing.T) {
	_, err := NewCipherWithRand(make([]byte, 16), bytes.NewReader(nil))
	if !errors.Is(err, ErrInvalidKeyLength) {
		t.Errorf("ожидалась ErrInvalidKeyLength, получено: %v", err)
	}
}

// TestEncryptHandlesEmptyPlaintext — пустой plaintext — валидный кейс
// (AES-GCM это поддерживает; формат всё равно nonce+tag).
func TestEncryptHandlesEmptyPlaintext(t *testing.T) {
	c, _ := NewCipher(validKey())
	ct, err := c.Encrypt([]byte{}, []byte("aad"))
	if err != nil {
		t.Fatalf("Encrypt пустого plaintext: %v", err)
	}
	// nonce(12) + 0 ct + tag(16) = 28 байт
	if len(ct) != 12+0+16 {
		t.Errorf("длина ciphertext пустого plaintext = %d, ожидалось 28", len(ct))
	}
	dec, err := c.Decrypt(ct, []byte("aad"))
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if len(dec) != 0 {
		t.Errorf("decrypted = %q, ожидалось пустое", dec)
	}
}

// TestNilCipherEncryptFails — методы на nil-Cipher не паникуют, а
// возвращают ошибку.
func TestNilCipherEncryptFails(t *testing.T) {
	var c *Cipher
	_, err := c.Encrypt([]byte("x"), nil)
	if !errors.Is(err, ErrCipherNotInitialized) {
		t.Errorf("ожидалась ErrCipherNotInitialized, получено: %v", err)
	}
	_, err = c.Decrypt([]byte("xxxxxxxxxxxxxxxxxxxxxxxxxxxx"), nil)
	if !errors.Is(err, ErrCipherNotInitialized) {
		t.Errorf("ожидалась ErrCipherNotInitialized, получено: %v", err)
	}
}

// TestEncryptRandReaderError — сбой источника случайности → ошибка.
type erroringReader struct{}

func (erroringReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestEncryptRandReaderError(t *testing.T) {
	c, _ := NewCipherWithRand(validKey(), erroringReader{})
	_, err := c.Encrypt([]byte("x"), nil)
	if err == nil {
		t.Fatal("ожидалась ошибка от сломанного rand-reader")
	}
}
