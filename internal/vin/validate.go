package vin

import (
	"fmt"
	"regexp"
)

var vinRegex = regexp.MustCompile(`^[A-HJ-NPR-Z0-9]{17}$`)

func Validate(v string) error {
	if !vinRegex.MatchString(v) {
		return fmt.Errorf("invalid VIN: must be exactly 17 characters, alphanumeric excluding I, O, Q")
	}
	return nil
}
