package persist

import (
	"errors"
	"slices"
	"testing"
)

var widgets = NewEntity("widgets", "name", "created_at", "name", "created_at")

func TestListArgsSort(t *testing.T) {
	cases := []struct {
		name      string
		args      ListArgs
		wantField string
		wantDesc  bool
		wantErr   error
	}{
		{"default sort is descending", ListArgs{}, "created_at", true, nil},
		{"default ignores direction", ListArgs{SortDir: "asc"}, "created_at", true, nil},
		{"ascending", ListArgs{SortBy: "name", SortDir: "asc"}, "name", false, nil},
		{"anything but asc is descending", ListArgs{SortBy: "name", SortDir: "up"}, "name", true, nil},
		{"unsortable field", ListArgs{SortBy: "secret"}, "", false, ErrInvalidSortField},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			field, desc, err := c.args.Sort(widgets)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if field != c.wantField || desc != c.wantDesc {
				t.Fatalf("got (%q, %v), want (%q, %v)", field, desc, c.wantField, c.wantDesc)
			}
		})
	}
}

func TestSlice(t *testing.T) {
	rows := []int{1, 2, 3, 4, 5}
	for _, c := range []struct {
		offset, limit int
		want          []int
	}{
		{0, 0, rows},
		{-1, -1, rows},
		{1, 2, []int{2, 3}},
		{3, 0, []int{4, 5}},
		{4, 5, []int{5}},
		{9, 2, []int{}},
	} {
		got := Slice(rows, c.offset, c.limit)
		if !slices.Equal(got.Rows, c.want) || got.Total != 5 {
			t.Errorf("offset %d limit %d: %+v", c.offset, c.limit, got)
		}
	}
}

func TestNewEntity(t *testing.T) {
	if widgets.Name != "widgets" || widgets.QField != "name" || widgets.DefaultSort != "created_at" {
		t.Fatalf("entity fields: %+v", widgets)
	}
}
