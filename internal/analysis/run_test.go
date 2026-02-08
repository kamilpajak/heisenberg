package analysis

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatRunDate_ValidRFC3339(t *testing.T) {
	result := formatRunDate("2026-02-08T18:04:50Z")
	assert.Equal(t, "2026-02-08", result)
}

func TestFormatRunDate_WithTimezone(t *testing.T) {
	result := formatRunDate("2026-01-15T10:30:00+01:00")
	assert.Equal(t, "2026-01-15", result)
}

func TestFormatRunDate_Empty(t *testing.T) {
	result := formatRunDate("")
	assert.Equal(t, "unknown date", result)
}

func TestFormatRunDate_InvalidFormat(t *testing.T) {
	result := formatRunDate("not-a-date")
	assert.Equal(t, "not-a-date", result)
}

func TestFormatRunDate_PartialDate(t *testing.T) {
	result := formatRunDate("2026-02-08")
	assert.Equal(t, "2026-02-08", result)
}
