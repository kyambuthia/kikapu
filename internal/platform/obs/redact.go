package obs

import (
	"regexp"
	"strings"
)

var phoneNumberPattern = regexp.MustCompile(`(\+?\d{1,3}[\s-]?)?\d{3,4}[\s-]?\d{3}[\s-]?\d{3,4}`)

func RedactPhoneNumber(phone string) string {
	if phone == "" {
		return ""
	}

	phone = strings.TrimSpace(phone)

	if len(phone) < 10 {
		return phone
	}

	matches := phoneNumberPattern.FindAllString(phone, -1)
	result := phone

	for _, match := range matches {
		cleaned := strings.ReplaceAll(match, " ", "")
		cleaned = strings.ReplaceAll(cleaned, "-", "")

		if len(cleaned) >= 10 {
			redacted := redactPhone(cleaned)
			result = strings.Replace(result, match, redacted, 1)
		}
	}

	return result
}

func redactPhone(phone string) string {
	if len(phone) < 10 {
		return phone
	}

	prefix := phone[:4]
	suffix := phone[len(phone)-3:]
	middle := strings.Repeat("*", len(phone)-7)

	return prefix + middle + suffix
}
