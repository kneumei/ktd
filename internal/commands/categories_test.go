package commands

import (
	"reflect"
	"testing"

	"ktd/internal/categories"
)

func TestChooseCategories(t *testing.T) {
	canon := categories.Build([][]string{{"MDMD", "Consumers"}, {"MDMD"}})

	tests := []struct {
		name     string
		stated   []string
		inferred []string
		want     []string
	}{
		{
			name:     "stated wins outright over inferred",
			stated:   []string{"mdmd"},
			inferred: []string{"mdmd", "Consumers"},
			want:     []string{"MDMD"},
		},
		{
			name:     "nothing stated falls back to inferred",
			inferred: []string{"Consumers"},
			want:     []string{"Consumers"},
		},
		{
			name:   "stated dupes collapse case-insensitively",
			stated: []string{"mdmd", "MDMD"},
			want:   []string{"MDMD"},
		},
		{
			name:     "unknown stated category keeps its own casing",
			stated:   []string{"newThing"},
			inferred: []string{"Consumers"},
			want:     []string{"newThing"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chooseCategories(canon, tt.stated, tt.inferred)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("chooseCategories() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
