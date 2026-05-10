package xui

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
)

// generateTOTP 按 RFC 6238 计算给定 base32 secret 在"当前时刻"的 6 位 TOTP 码。
//
// 算法（RFC 6238 §4.2 + RFC 4226 §5.3 动态截断）：
//
//	1. 把 base32 secret 解码成原始 key（任意字节数；3x-ui / Google Authenticator
//	   常见为 16 / 24 / 32 字节）；
//	2. 取 counter = floor(now_unix / 30)（Step Size = 30s）；
//	3. 把 counter 按大端 8 字节拼接，HMAC-SHA1(key, counter) 得 20 字节摘要；
//	4. 取 sum[19] & 0x0F 作 offset；从 sum[offset..offset+3] 取 4 字节，
//	   首字节屏蔽最高位（防止视为负数），合并成 31-bit 无符号整数；
//	5. 6 位码 = 该整数 mod 1_000_000，前导零补齐。
//
// 设计取舍：
//
//	a) 仅依赖标准库（crypto/hmac + crypto/sha1 + encoding/base32 + encoding/binary +
//	   time），不引 pquerna/otp 等第三方包。这与本项目"标准库优先"原则一致
//	   （go.mod 仅留 golang.org/x/crypto + golang.org/x/term + modernc.org/sqlite）。
//	b) 不接受调用方自定义 step/digits/algorithm 参数：3x-ui 的 LoginForm 仅支持
//	   默认参数（30s / 6位 / SHA1）；引入参数化只会增加误用面，需要时再扩展。
//	c) 使用 time.Now().Unix() 而非传 ctx：上层 doLogin 已有 ctx 控制 HTTP 超时，
//	   计算 TOTP 不会阻塞，无需 ctx；保持函数纯粹便于单测。
//
// 失败原因（按发生概率从高到低）：
//
//	- secret 为空或仅含 = 填充符（运维误填）；
//	- secret 含非 base32 合法字符（A-Z 与 2-7 之外）；
//	- secret 长度不正确（base32 NoPadding 要求总位数能整除 8）。
//
// 时钟漂移（已知风险）：若 VPS 与 3x-ui 主机系统时钟相差超过 ±30s，服务端
// 会判定该码无效；这属于运维基础设施职责（NTP 同步），代码层不补偿——补偿
// 算法（如多窗口尝试）会让攻击者拥有更长的暴力破解窗口，且仍掩盖不了"时钟
// 没同步"这个根因。文档（README + roadmap §5.7）已明确该约束。
func generateTOTP(secret string) (string, error) {
	// 与 config.Validate 中的"轻量校验"保持完全相同的清洗规则：
	// ToUpper（base32 标准编码全大写）+ TrimRight "="（兼容 NoPadding 编码）。
	// 不在此处再 TrimSpace 是因为 config.Validate 已 trim 过；运行期再 trim
	// 既是冗余也无害——选择不做以维持函数纯粹。
	cleaned := strings.ToUpper(strings.TrimRight(secret, "="))
	if cleaned == "" {
		return "", errors.New("TOTP secret 为空或仅含 = 填充字符")
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(cleaned)
	if err != nil {
		return "", fmt.Errorf("解码 base32 secret：%w", err)
	}

	// counter = floor(unix_seconds / 30)，大端 8 字节。
	counter := uint64(time.Now().Unix() / 30)
	var counterBytes [8]byte
	binary.BigEndian.PutUint64(counterBytes[:], counter)

	// HMAC-SHA1 输出固定 20 字节；Write 不会失败，错误返回也忽略。
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(counterBytes[:])
	sum := mac.Sum(nil)

	// 动态截断：取最后一字节低 4 位作 offset，从 sum 中取 4 字节合并成 31-bit
	// 无符号整数；最高位屏蔽 0x7F 避免被符号位干扰（RFC 4226 §5.3 明确要求）。
	offset := sum[len(sum)-1] & 0x0F
	code := (uint32(sum[offset])&0x7F)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])

	// 6 位码（不足前补零）。1_000_000 = 10^6；不写成常量是因为本函数硬编码
	// 6 位（与 3x-ui 服务端默认一致），扩展性留给未来如真有需要时再做。
	return fmt.Sprintf("%06d", code%1_000_000), nil
}
