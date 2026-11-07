package commands

import (
	"reflect"
	"testing"

	"ktd/internal/categories"
)

func TestChooseCategories(t *testing.T) {
	canon := categories.Build([][]string{{"MDMD", "Consumers"}, {"MDMD"}})
	allowed := []string{"Cadence", "PacificLife", "MDHD", "ExportDatabase"}

	tests := []struct {
		name     string
		stated   []string
		inferred []string
		want     []string
	}{
		{
			name:     "stated wins outright over inferred",
			stated:   []string{"mdmd"},
			inferred: []string{"Cadence"},
			want:     []string{"MDMD"},
		},
		{
			name:     "nothing stated falls back to first allowed inferred",
			inferred: []string{"Consumers", "pacificlife", "Cadence"},
			want:     []string{"PacificLife"},
		},
		{
			name:     "inferred outside the allowed set is dropped",
			inferred: []string{"Consumers"},
			want:     nil,
		},
		{
			name:   "stated allowed category takes the allowed casing",
			stated: []string{"exportdatabase"},
			want:   []string{"ExportDatabase"},
		},
		{
			name:   "stated dupes collapse case-insensitively",
			stated: []string{"mdmd", "MDMD"},
			want:   []string{"MDMD"},
		},
		{
			name:     "unknown stated category keeps its own casing",
			stated:   []string{"newThing"},
			inferred: []string{"Cadence"},
			want:     []string{"newThing"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chooseCategories(canon, allowed, tt.stated, tt.inferred)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("chooseCategories() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
