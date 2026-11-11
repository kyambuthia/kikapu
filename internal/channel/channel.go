package channel

import (
	"errors"
	"regexp"
	"strings"
)

var (
	ErrInvalidID     = errors.New("channel: invalid ID")
	ErrInvalidName   = errors.New("channel: invalid name")
	ErrInvalidSlug   = errors.New("channel: invalid slug")
	ErrInvalidRegion = errors.New("channel: invalid region ID")
	slugRegex        = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

type SalesChannel struct {
	ID      string
	Name    string
	Slug    string
	Regions []string
}

func (ch SalesChannel) Validate() error {
	if ch.ID == "" {
		return ErrInvalidID
	}
	if ch.Name == "" {
		return ErrInvalidName
	}
	if !isValidSlug(ch.Slug) {
		return ErrInvalidSlug
	}
	for _, r := range ch.Regions {
		if r == "" {
			return ErrInvalidRegion
		}
	}
	return nil
}

func isValidSlug(slug string) bool {
	if len(slug) == 0 || len(slug) > 80 {
		return false
	}
	if strings.HasPrefix(slug, "-") || strings.HasSuffix(slug, "-") {
		return false
	}
	return slugRegex.MatchString(slug)
}

type Repository interface {
	Get(ctx interface{}, id string) (*SalesChannel, error)
	GetBySlug(ctx interface{}, slug string) (*SalesChannel, error)
	List(ctx interface{}) ([]SalesChannel, error)
}
