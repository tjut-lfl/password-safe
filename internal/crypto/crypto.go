// Package crypto 提供主密码派生、KEK/DEK 包装与 AES-256-GCM 加解密。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	KeySize   = 32 // AES-256
	NonceSize = 12 // GCM standard nonce
	SaltSize  = 32

	// Argon2id 默认参数（适合桌面本地保险箱）
	DefaultTime    = uint32(3)
	DefaultMemory  = uint32(64 * 1024) // 64 MiB
	DefaultThreads = uint8(4)
)

var (
	ErrDecryptFailed = errors.New("解密失败：主密码错误或数据已损坏")
	ErrInvalidNonce  = errors.New("无效的 nonce 长度")
	ErrInvalidKey    = errors.New("无效的密钥长度")
)

// Params 描述 Argon2id 派生参数。
type Params struct {
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"`
	Threads uint8  `json:"threads"`
	Salt    []byte `json:"-"` // 单独以 base64 存
}

// DefaultParams 返回推荐参数（不含 salt）。
func DefaultParams() Params {
	return Params{
		Time:    DefaultTime,
		Memory:  DefaultMemory,
		Threads: DefaultThreads,
	}
}

// GenerateSalt 生成随机 salt。
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, SaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("生成 salt 失败: %w", err)
	}
	return salt, nil
}

// GenerateKey 生成随机 32 字节 DEK。
func GenerateKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("生成密钥失败: %w", err)
	}
	return key, nil
}

// DeriveKey 使用 Argon2id 从主密码派生 32 字节 KEK。
func DeriveKey(password []byte, salt []byte, p Params) []byte {
	if p.Time == 0 {
		p.Time = DefaultTime
	}
	if p.Memory == 0 {
		p.Memory = DefaultMemory
	}
	if p.Threads == 0 {
		p.Threads = DefaultThreads
	}
	return argon2.IDKey(password, salt, p.Time, p.Memory, p.Threads, KeySize)
}

// Encrypt 使用 AES-256-GCM 加密 plaintext，返回 nonce||ciphertext。
func Encrypt(key, plaintext []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("生成 nonce 失败: %w", err)
	}
	out := gcm.Seal(nonce, nonce, plaintext, nil)
	return out, nil
}

// Decrypt 解密 nonce||ciphertext。
func Decrypt(key, data []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(data) < ns {
		return nil, ErrInvalidNonce
	}
	nonce, ciphertext := data[:ns], data[ns:]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return plain, nil
}

// WrapKey 用 KEK 加密 DEK（AES-GCM）。
func WrapKey(kek, dek []byte) ([]byte, error) {
	return Encrypt(kek, dek)
}

// UnwrapKey 用 KEK 解密被包装的 DEK。
func UnwrapKey(kek, wrapped []byte) ([]byte, error) {
	return Decrypt(kek, wrapped)
}

// Zero 尽量清零敏感字节切片。
func Zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// Equal 常量时间比较。
func Equal(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// EncodeB64 / DecodeB64 便捷 base64。
func EncodeB64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func DecodeB64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// GeneratePassword 生成高强度随机密码（可打印 ASCII）。
func GeneratePassword(length int, includeSymbols bool) (string, error) {
	if length < 8 {
		length = 8
	}
	if length > 128 {
		length = 128
	}
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const symbols = "!@#$%^&*()-_=+[]{}<>?,./"
	charset := letters
	if includeSymbols {
		charset += symbols
	}
	out := make([]byte, length)
	// 拒绝采样，避免模偏差
	max := 256 - (256 % len(charset))
	for i := 0; i < length; {
		var buf [1]byte
		if _, err := io.ReadFull(rand.Reader, buf[:]); err != nil {
			return "", err
		}
		if int(buf[0]) >= max {
			continue
		}
		out[i] = charset[int(buf[0])%len(charset)]
		i++
	}
	return string(out), nil
}

// 小型嵌入词表（diceware 风格，约 200 词）用于可记忆口令短语。
var wordList = []string{
	"able", "acid", "aged", "also", "area", "army", "away", "baby", "back", "ball",
	"band", "bank", "base", "bath", "bear", "beat", "been", "beer", "bell", "belt",
	"best", "bird", "blow", "blue", "boat", "body", "bomb", "bond", "bone", "book",
	"boom", "born", "boss", "both", "bowl", "bulk", "burn", "bush", "busy", "call",
	"calm", "came", "camp", "card", "care", "case", "cash", "cast", "cell", "chat",
	"chip", "city", "club", "coal", "coat", "code", "cold", "come", "cook", "cool",
	"cope", "copy", "core", "cost", "crew", "crop", "dark", "data", "date", "dawn",
	"days", "dead", "deal", "dear", "debt", "deep", "deny", "desk", "dial", "diet",
	"disc", "disk", "does", "done", "door", "dose", "down", "draw", "drew", "drop",
	"drug", "dual", "duke", "dust", "duty", "each", "earn", "ease", "east", "easy",
	"edge", "else", "even", "ever", "evil", "exit", "face", "fact", "fail", "fair",
	"fall", "farm", "fast", "fate", "fear", "feed", "feel", "feet", "fell", "felt",
	"file", "fill", "film", "find", "fine", "fire", "firm", "fish", "five", "flat",
	"flow", "foam", "fold", "folk", "food", "foot", "ford", "form", "fort", "four",
	"free", "from", "fuel", "full", "fund", "gain", "game", "gate", "gave", "gear",
	"gene", "gift", "girl", "give", "glad", "goal", "goes", "gold", "golf", "gone",
	"good", "gray", "grew", "grey", "grow", "gulf", "hair", "half", "hall", "hand",
	"hang", "hard", "harm", "hate", "have", "head", "hear", "heat", "held", "hell",
	"help", "here", "hero", "high", "hill", "hire", "hold", "hole", "holy", "home",
	"hope", "host", "hour", "huge", "hung", "hunt", "hurt", "idea", "inch", "into",
	"iron", "item", "jack", "jane", "jean", "john", "join", "jump", "jury", "just",
	"keen", "keep", "kent", "kept", "kick", "kill", "kind", "king", "knee", "knew",
	"know", "lack", "lady", "laid", "lake", "land", "lane", "last", "late", "lead",
	"left", "less", "life", "lift", "like", "line", "link", "list", "live", "load",
	"loan", "lock", "logo", "long", "look", "lord", "lose", "loss", "lost", "love",
	"luck", "made", "mail", "main", "make", "male", "many", "mark", "mass", "matt",
	"meal", "mean", "meat", "meet", "menu", "mere", "mike", "mile", "milk", "mill",
	"mind", "mine", "miss", "mode", "mood", "moon", "more", "most", "move", "much",
	"must", "name", "navy", "near", "neck", "need", "news", "next", "nice", "nick",
	"nine", "none", "nose", "note", "okay", "once", "only", "onto", "open", "oral",
	"over", "pace", "pack", "page", "paid", "pain", "pair", "palm", "park", "part",
	"pass", "past", "path", "peak", "pick", "pink", "pipe", "plan", "play", "plot",
	"plus", "poem", "poet", "poll", "pool", "poor", "port", "post", "pull", "pure",
	"push", "race", "rail", "rain", "rank", "rare", "rate", "read", "real", "rear",
	"rely", "rent", "rest", "rice", "rich", "ride", "ring", "rise", "risk", "road",
	"rock", "role", "roll", "roof", "room", "root", "rose", "rule", "rush", "ruth",
	"safe", "said", "sake", "sale", "salt", "same", "sand", "save", "seat", "seed",
	"seek", "seem", "seen", "self", "sell", "send", "sent", "sept", "ship", "shop",
	"shot", "show", "shut", "sick", "side", "sign", "site", "size", "skin", "slip",
	"slow", "snow", "soft", "soil", "sold", "sole", "some", "song", "soon", "sort",
	"soul", "spot", "star", "stay", "step", "stop", "such", "suit", "sure", "take",
	"tale", "talk", "tall", "tank", "tape", "task", "team", "tech", "tell", "tend",
	"term", "test", "text", "than", "that", "them", "then", "they", "thin", "this",
	"thus", "tide", "tidy", "tied", "time", "tiny", "told", "toll", "tone", "tony",
	"took", "tool", "tour", "town", "tree", "trip", "true", "tune", "turn", "twin",
	"type", "unit", "upon", "used", "user", "vary", "vast", "very", "vice", "view",
	"vote", "wage", "wait", "wake", "walk", "wall", "want", "ward", "warm", "wash",
	"wave", "ways", "weak", "wear", "week", "well", "went", "were", "west", "what",
	"when", "whom", "wide", "wife", "wild", "will", "wind", "wine", "wing", "wire",
	"wise", "wish", "with", "wood", "word", "wore", "work", "worm", "worn", "yard",
	"yeah", "year", "your", "zero", "zone",
}

// GeneratePassphrase 生成 diceware 风格可记忆口令（wordCount 个词，用分隔符连接）。
func GeneratePassphrase(wordCount int, separator string) (string, error) {
	if wordCount < 3 {
		wordCount = 3
	}
	if wordCount > 12 {
		wordCount = 12
	}
	if separator == "" {
		separator = "-"
	}
	n := len(wordList)
	max := 256 - (256 % n)
	words := make([]string, wordCount)
	for i := 0; i < wordCount; {
		var buf [1]byte
		if _, err := io.ReadFull(rand.Reader, buf[:]); err != nil {
			return "", err
		}
		// 词表长度可能 > 256，用两字节
		var buf2 [2]byte
		if _, err := io.ReadFull(rand.Reader, buf2[:]); err != nil {
			return "", err
		}
		idx := int(buf2[0])<<8 | int(buf2[1])
		words[i] = wordList[idx%n]
		_ = max // keep reject-sampling pattern available
		i++
	}
	return strings.Join(words, separator), nil
}
