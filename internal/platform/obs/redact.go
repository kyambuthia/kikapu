package obs

import (
	"regexp"
	"strings"
)

var phonePattern = regexp.MustCompile(`^\+?[0-9]{10,15}$`)

func RedactPhone(phone string) string {
	if phone == "" {
		return ""
	}

	cleaned := strings.ReplaceAll(phone, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "-", "")

	if !phonePattern.MatchString(cleaned) {
		return phone
	}

	prefix := ""
	if strings.HasPrefix(cleaned, "+") {
		prefix = "+"
		cleaned = cleaned[1:]
	}

	if len(cleaned) < 10 {
		return phone
	}

	countryCode := cleaned[:3]
	lastThree := cleaned[len(cleaned)-3:]

	return prefix + countryCode + "****" + lastThree
}

func RedactPAN(pan string) string {
	if len(pan) < 8 {
		return pan
	}
	cleaned := strings.ReplaceAll(pan, " ", "")
	if len(cleaned) < 8 {
		return pan
	}
	return cleaned[:4] + "****" + cleaned[len(cleaned)-4:]
}
