package channels

import "testing"

func TestCategoryValid(t *testing.T) {
	tests := []struct {
		name string
		cat  Category
		want bool
	}{
		{"longform valid", Longform, true},
		{"digest valid", Digest, true},
		{"brief valid", Brief, true},
		{"empty invalid", Category(""), false},
		{"unknown invalid", Category("thread"), false},
		{"case-sensitive invalid", Category("Longform"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cat.Valid(); got != tt.want {
				t.Errorf("%q.Valid() = %v, want %v", tt.cat, got, tt.want)
			}
		})
	}
}
