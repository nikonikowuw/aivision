package errno

import "testing"

func TestMessageByLanguage(t *testing.T) {
	tests := []struct {
		name string
		lang string
		code int
		want string
	}{
		{"zh-CN hit", "zh-CN", CodeBadCredential, "用户名或密码错误"},
		{"zh-TW hit", "zh-TW", CodeBadCredential, "使用者名稱或密碼錯誤"},
		{"en-US hit", "en-US", CodeBadCredential, "Invalid username or password"},
		{"empty lang falls back to default", "", CodeBadCredential, "用户名或密码错误"},
		{"unknown lang falls back to default", "fr-FR", CodeBadCredential, "用户名或密码错误"},
		{"unknown code zh", "zh-CN", 9999, "未知错误"},
		{"unknown code zh-tw", "zh-TW", 9999, "未知錯誤"},
		{"unknown code en", "en-US", 9999, "Unknown error"},
		{"success message invariant", "en-US", CodeOK, "ok"},
		{"success message invariant zh-tw", "zh-TW", CodeOK, "ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Message(tt.lang, tt.code); got != tt.want {
				t.Errorf("Message(%q, %d) = %q, want %q", tt.lang, tt.code, got, tt.want)
			}
		})
	}
}

func TestLangFromHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"empty", "", DefaultLang},
		{"single zh-CN", "zh-CN", "zh-CN"},
		{"single zh-TW", "zh-TW", "zh-TW"},
		{"single zh-HK", "zh-HK", "zh-TW"},
		{"single zh-Hant", "zh-Hant", "zh-TW"},
		{"single en-US", "en-US", "en-US"},
		{"browser list zh", "zh-CN,zh;q=0.9,en;q=0.8", "zh-CN"},
		{"browser list zh-tw", "zh-TW,zh;q=0.9,en;q=0.8", "zh-TW"},
		{"browser list en", "en-US,en;q=0.9", "en-US"},
		{"q weight only", "en;q=0.9", "en-US"},
		{"base tag en", "en", "en-US"},
		{"base tag zh", "zh", "zh-CN"},
		{"case-insensitive", "EN-us", "en-US"},
		{"case-insensitive zh-tw", "ZH-tw", "zh-TW"},
		{"surrounding spaces", "  zh-CN , zh;q=0.8", "zh-CN"},
		{"unsupported falls back", "fr-FR,fr;q=0.9", DefaultLang},
		{"wildcard", "*", DefaultLang},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LangFromHeader(tt.header); got != tt.want {
				t.Errorf("LangFromHeader(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestMessagePartialLangMapFallsBack(t *testing.T) {
	// 注入一张不完整的语言表（缺 unknownCode，且须含 CodeOK 以免破坏
	// 成功文案不变量测试）；未知码应回退到 DefaultLang 的兜底文案。
	messages["fr-FR"] = map[int]string{
		CodeOK:           "ok",
		CodeUserNotFound: "Utilisateur introuvable",
	}
	defer delete(messages, "fr-FR")

	if got := Message("fr-FR", CodeUserNotFound); got != "Utilisateur introuvable" {
		t.Errorf("hit = %q, want Utilisateur introuvable", got)
	}
	want := messages[DefaultLang][unknownCode]
	if got := Message("fr-FR", 9999); got != want {
		t.Errorf("unknown = %q, want %q", got, want)
	}
}

func TestSuccessMessageInvariantAcrossLanguages(t *testing.T) {
	// 成功文案全语言统一为 "ok"（response.OK 依赖此不变量）。
	for lang := range messages {
		if got := Message(lang, CodeOK); got != "ok" {
			t.Errorf("Message(%q, CodeOK) = %q, want ok", lang, got)
		}
	}
}
