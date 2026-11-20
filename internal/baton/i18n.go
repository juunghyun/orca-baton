package baton

import (
	"os"
	"strings"
)

var lang = "" // Main 이 설정을 읽은 뒤 정한다

func setLang(cfgLang string) {
	lang = cfgLang
	if lang == "" {
		lang = "en"
		if strings.HasPrefix(os.Getenv("LANG"), "ko") || strings.HasPrefix(os.Getenv("LC_ALL"), "ko") {
			lang = "ko"
		}
	}
}

// L 은 화면 문구를 고른다: L("English", "한국어").
func L(en, ko string) string {
	if lang == "ko" {
		return ko
	}
	return en
}
