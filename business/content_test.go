package business

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContentIdentity(t *testing.T) {
	tests := []struct {
		name, link, guid string
		opts             IdentityOpts
		want             string
	}{
		{"a link is its own identity", "https://example.org/a", "g1", IdentityOpts{}, "https://example.org/a"},
		{"the fragment is no part of it", "https://example.org/a#comments", "g1", IdentityOpts{}, "https://example.org/a"},
		{"two fragments, one item", "https://example.org/a#top", "g2", IdentityOpts{}, "https://example.org/a"},
		{"without a link the GUID is the identity", "", "tag:example.org,2026:1", IdentityOpts{}, "tag:example.org,2026:1"},
		{"neither link nor GUID", "", "", IdentityOpts{}, ""},
		{"a query stays when nothing is dropped", "https://example.org/?p=123", "", IdentityOpts{}, "https://example.org/?p=123"},
		{"another number is another item", "https://example.org/?p=124", "", IdentityOpts{}, "https://example.org/?p=124"},
		{"a listed parameter is dropped", "https://example.org/a?wt_mc=rss.red", "", IdentityOpts{DropQueryParams: []string{"wt_mc"}}, "https://example.org/a"},
		{"the rest of the query stays, in its order", "https://example.org/?p=7&wt_mc=x&b=2", "", IdentityOpts{DropQueryParams: []string{"wt_mc"}}, "https://example.org/?p=7&b=2"},
		{"several parameters", "https://example.org/a?utm_source=x&id=5&utm_medium=y", "", IdentityOpts{DropQueryParams: []string{"utm_source", "utm_medium"}}, "https://example.org/a?id=5"},
		{"a parameter without a value", "https://example.org/a?wt_mc&id=5", "", IdentityOpts{DropQueryParams: []string{"wt_mc"}}, "https://example.org/a?id=5"},
		{"names are exact", "https://example.org/a?wt_mc2=x&xwt_mc=y", "", IdentityOpts{DropQueryParams: []string{"wt_mc"}}, "https://example.org/a?wt_mc2=x&xwt_mc=y"},
		{"dropping and the fragment together", "https://example.org/a?wt_mc=x#top", "", IdentityOpts{DropQueryParams: []string{"wt_mc"}}, "https://example.org/a"},
		{"an escaped name is still the name", "https://example.org/a?wt%5Fmc=x", "", IdentityOpts{DropQueryParams: []string{"wt_mc"}}, "https://example.org/a"},
		{"a list does not touch the GUID", "", "g1?wt_mc=x", IdentityOpts{DropQueryParams: []string{"wt_mc"}}, "g1?wt_mc=x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ContentIdentity(tt.link, tt.guid, tt.opts))
		})
	}
}
