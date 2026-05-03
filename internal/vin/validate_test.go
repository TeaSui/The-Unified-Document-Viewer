package vin_test

import (
	"testing"

	"github.com/tungnguyen/unified-document-viewer/internal/vin"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid VIN", input: "1HGCM82633A004352", wantErr: false},
		{name: "valid VIN all digits", input: "12345678901234567", wantErr: false},
		{name: "valid VIN all letters", input: "ABCDEFGHJKLMNPRST", wantErr: false},
		{name: "too short", input: "1HGCM826", wantErr: true},
		{name: "too long", input: "1HGCM82633A0043521", wantErr: true},
		{name: "empty", input: "", wantErr: true},
		{name: "contains I", input: "1HGCM82633I004352", wantErr: true},
		{name: "contains O", input: "1HGCM82633O004352", wantErr: true},
		{name: "contains Q", input: "1HGCM82633Q004352", wantErr: true},
		{name: "contains lowercase", input: "1hgcm82633a004352", wantErr: true},
		{name: "contains space", input: "1HGCM82633 004352", wantErr: true},
		{name: "contains special char", input: "1HGCM82633A00435!", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := vin.Validate(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}
