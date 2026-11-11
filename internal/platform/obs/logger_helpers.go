package obs

import (
	"encoding/json"
	"regexp"
	"strings"
)

type Redacted struct {
	Original string
	Redacted string
}

func RedactPhoneNumbers(input string) string {
	phoneRegex := regexp.MustCompile(`(?P<country>[+]?254)\s*(?P<prefix>\d{4})\s*(?P<last>\d{3})`)
	phoneRegexLong := regexp.MustCompile(`(?P<country>[+]?254)\s*(?P<prefix>\d{4})\s*(?P<last>\d{4})|(?P<country2>[+]?254)\s*(?P<phone>\d{9})`)

	if phoneRegex.MatchString(input) {
		return phoneRegex.ReplaceAllString(input, "${country} **** ${last}")
	}

	if phoneRegexLong.MatchString(input) {
		return phoneRegexLong.ReplaceAllString(input, "${country} **** ${last}")
	}

	return input
}

func RedactPAN(input string) string {
	panRegex := regexp.MustCompile(`(?P<start>\d{1,4})?\s*(?P<middle>\d{4})\s*(?P<last>\d{4})`)

	if panRegex.MatchString(input) {
		r := panRegex.ReplaceAllString(input, "${start} **** ${middle} **** ${last}")
		return strings.ReplaceAll(r, " **** **** ", " ")
	}

	if len(input) >= 16 {
		return input[:4] + " ******** " + input[len(input)-4:]
	}

	return input
}

func RedactSensitiveFields[T any](input T) ([]string, T) {
	jsonData, err := json.Marshal(input)
	if err != nil {
		return nil, input
	}

	var result map[string]any
	if err := json.Unmarshal(jsonData, &result); err != nil {
		return nil, input
	}

	sensitiveFields := []string{"phone", "telephone", "mobile", "whatsapp", "email", "token", "secret", "password", "pin", "cvv"}

	redacted := make(map[string]any)
	for k, v := range result {
		lowerK := strings.ToLower(k)

		if contains(sensitiveFields, lowerK) {
			if strVal, ok := v.(string); ok {
				if strings.HasPrefix(strVal, "+254") || strings.Contains(strVal, "254") {
					redacted[k] = RedactPhoneNumbers(strVal)
				} else if len(strVal) >= 16 || strings.Contains(strVal, "-") {
					redacted[k] = RedactPAN(strVal)
				} else {
					redacted[k] = "REDACTED"
				}
			} else {
				redacted[k] = "REDACTED"
			}
		} else {
			redacted[k] = v
		}
	}

	redactedJSON, err := json.Marshal(redacted)
	if err != nil {
		return nil, input
	}

	var redactedStruct T
	if err := json.Unmarshal(redactedJSON, &redactedStruct); err != nil {
		return nil, input
	}

	return []string{k}, redactedStruct
}

func contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}

func MaskPANLikeString(input string) string {
	if len(input) == 0 {
		return input
	}

	result := []rune{}
	for i, ch := range input {
		if i > 4 && (i < len(input)-4 || (i == len(input)-4 && ch != '-')) {
			if ch == '-' {
				result = append(result, '-')
			} else if ch >= '0' && ch <= '9' {
				result = append(result, '*')
			} else {
				result = append(result, ch)
			}
		} else {
			result = append(result, ch)
		}
	}

	return string(result)
}
