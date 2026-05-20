// Package crypto — симметричное шифрование mapping-значений
// (AES-256-GCM, env-ключ MAPPING_ENCRYPTION_KEY).
//
// Шифр используется для записей pseudonym_mappings.raw_value_encrypted:
// шифруется raw-значение каждой сущности (см. iteration-9.md §Р1).
// Формат ciphertext: nonce(12) || ct || GCM-tag(16) — self-contained.
//
// AAD (Additional Authenticated Data) обязателен — связывает ciphertext
// с контекстом mapping'а (sha256(session_id || pseudonym)[:16]),
// защищает от swap-атак на уровне БД (см. iteration-9.md §Р1, AAD-цикл).
//
// Инвариант «никакого raw в логах»: Cipher и его методы НИКОГДА не
// логируют plaintext/ciphertext. Ошибки оборачиваются без содержимого.
package crypto

import (
	"crypto/cipher"
	"errors"
	"io"
)

// keyLen — длина ключа в байтах (AES-256).
const keyLen = 32

// ErrInvalidKeyLength — ключ не 32 байта (AES-256 требует ровно 256 бит).
var ErrInvalidKeyLength = errors.New("crypto: ключ должен быть 32 байта (AES-256)")

// ErrCipherNotInitialized — попытка Encrypt/Decrypt на nil-Cipher.
var ErrCipherNotInitialized = errors.New("crypto: cipher не инициализирован")

// ErrCiphertextTooShort — переданные данные короче минимально допустимых
// (nonce + GCM-tag) — это значит, что они не могут быть валидным выходом
// Encrypt этого пакета.
var ErrCiphertextTooShort = errors.New("crypto: ciphertext короче минимальной длины")

// Cipher — AES-256-GCM с инжектируемым источником случайности для nonce.
// Nil-значение Cipher — невалидно; всегда строить через NewCipher.
type Cipher struct {
	gcm  cipher.AEAD
	rand io.Reader
}

// NewCipher строит Cipher из 32-байтового ключа. Источник случайности —
// crypto/rand.Reader (для детерминизма в тестах есть NewCipherWithRand).
func NewCipher(key []byte) (*Cipher, error) {
	return nil, errors.New("crypto: not implemented (Ф1 red)")
}

// NewCipherWithRand — то же, что NewCipher, но позволяет инжектировать
// источник случайности (тесты на nonce-uniqueness).
func NewCipherWithRand(key []byte, randReader io.Reader) (*Cipher, error) {
	return nil, errors.New("crypto: not implemented (Ф1 red)")
}

// Encrypt шифрует plaintext под AAD. Формат: nonce(12) || ct || tag(16).
// AAD должен передаваться неизменным в Decrypt; иначе расшифровка
// упадёт ошибкой аутентификации GCM.
func (c *Cipher) Encrypt(plaintext, aad []byte) ([]byte, error) {
	return nil, errors.New("crypto: not implemented (Ф1 red)")
}

// Decrypt расшифровывает данные, ранее полученные через Encrypt с теми
// же AAD. Возвращает ErrCiphertextTooShort если данные явно невалидны;
// в остальных случаях — обёрнутую ошибку GCM (тампер/неверный ключ/AAD).
func (c *Cipher) Decrypt(ciphertext, aad []byte) ([]byte, error) {
	return nil, errors.New("crypto: not implemented (Ф1 red)")
}
